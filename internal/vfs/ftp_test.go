package vfs

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestFTPLegacyCommonNameRequiresExplicitOptIn(t *testing.T) {
	standardURL, err := url.Parse("ftpes://legacy.example/")
	if err != nil {
		t.Fatal(err)
	}
	standard := ftpTLSConfig(standardURL)
	if standard.InsecureSkipVerify || standard.VerifyConnection != nil {
		t.Fatal("standard FTPS unexpectedly enabled custom legacy verification")
	}

	legacyURL, err := url.Parse("ftpes://legacy.example/?tls-legacy-common-name=true")
	if err != nil {
		t.Fatal(err)
	}
	legacy := ftpTLSConfig(legacyURL)
	if !legacy.InsecureSkipVerify || legacy.VerifyConnection == nil {
		t.Fatal("explicit legacy FTPS did not install custom verification")
	}
}

type closingReader struct {
	io.Reader
	err error
}

func (r closingReader) Close() error { return r.err }

func TestFTPDownloadReturnsFinalTransferError(t *testing.T) {
	finalErr := errors.New("server rejected completed transfer")
	response := closingReader{Reader: strings.NewReader("partial log"), err: finalErr}
	var destination strings.Builder
	err := copyFTPResponse(context.Background(), response, &destination, nil)
	if !errors.Is(err, finalErr) || destination.String() != "partial log" {
		t.Fatalf("download result = %q, error %v", destination.String(), err)
	}
}

func TestValidateDownloadedSizeRejectsSilentEmptyTransfer(t *testing.T) {
	if err := ValidateDownloadedSize(128, 0); err == nil {
		t.Fatal("silent empty transfer was accepted")
	}
	if err := ValidateDownloadedSize(0, 0); err != nil {
		t.Fatalf("real empty file was rejected: %v", err)
	}
}

func TestVerifyLegacyCommonNameValidatesChainAndExactName(t *testing.T) {
	state, roots := legacyCertificateState(t, "legacy.example", nil)
	if err := verifyLegacyCommonName(state, "legacy.example", roots); err != nil {
		t.Fatalf("valid legacy certificate rejected: %v", err)
	}
	if err := verifyLegacyCommonName(state, "other.example", roots); err == nil {
		t.Fatal("legacy certificate accepted for a different hostname")
	}
	err := verifyLegacyCommonName(state, "legacy.example", x509.NewCertPool())
	if err == nil {
		t.Fatal("legacy certificate accepted without a trusted CA chain")
	}
	var untrusted *UntrustedCertificateError
	if !errors.As(err, &untrusted) || len(untrusted.Fingerprint) != 64 {
		t.Fatalf("unknown CA error = %T %v", err, err)
	}
	if err := verifyPinnedCertificate(state, "legacy.example", untrusted.Fingerprint, true); err != nil {
		t.Fatalf("exact certificate pin rejected: %v", err)
	}
	if err := verifyPinnedCertificate(state, "legacy.example", "0000000000000000000000000000000000000000000000000000000000000000", true); err == nil {
		t.Fatal("incorrect certificate pin was accepted")
	}
	if !matchLegacyCommonName("*.legacy.example", "files.legacy.example") {
		t.Fatal("single-label legacy wildcard did not match")
	}
	if matchLegacyCommonName("*.legacy.example", "nested.files.legacy.example") {
		t.Fatal("legacy wildcard matched multiple hostname labels")
	}
}

func TestVerifyLegacyCommonNameNeverOverridesSAN(t *testing.T) {
	state, roots := legacyCertificateState(t, "legacy.example", []string{"different.example"})
	if err := verifyLegacyCommonName(state, "legacy.example", roots); err == nil {
		t.Fatal("legacy Common Name overrode a certificate SAN")
	}
	if err := verifyPinnedCertificate(state, "legacy.example", certificateFingerprint(state.PeerCertificates[0]), true); err == nil {
		t.Fatal("pinned certificate Common Name overrode its SAN")
	}
}

func legacyCertificateState(t *testing.T, commonName string, dnsNames []string) (tls.ConnectionState, *x509.CertPool) {
	t.Helper()
	now := time.Now()
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Test Root"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, &rootKey.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	root, err := x509.ParseCertificate(rootDER)
	if err != nil {
		t.Fatal(err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: commonName}, DNSNames: dnsNames,
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, root, &leafKey.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(leafDER)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(root)
	return tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf, root}}, roots
}
