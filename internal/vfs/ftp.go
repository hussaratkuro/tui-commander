package vfs

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/asn1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/url"
	pathpkg "path"
	"strings"
	"time"

	ftpclient "github.com/jlaffaye/ftp"
)

type FTP struct {
	client *ftpclient.ServerConn
	id     string
	label  string
}

// UntrustedCertificateError carries the exact leaf-certificate identity so the
// UI can offer an explicit SHA-256 pin instead of disabling TLS verification.
type UntrustedCertificateError struct {
	CommonName  string
	Fingerprint string
	Err         error
}

func (e *UntrustedCertificateError) Error() string {
	return fmt.Sprintf("certificate %q is signed by an unknown authority (SHA-256 %s): %v", e.CommonName, e.Fingerprint, e.Err)
}

func (e *UntrustedCertificateError) Unwrap() error { return e.Err }

func NewFTP(ctx context.Context, parsed *url.URL, password string) (*FTP, error) {
	host := parsed.Hostname()
	if host == "" {
		return nil, fmt.Errorf("FTP location has no host")
	}
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "ftps+implicit" {
			port = "990"
		} else {
			port = "21"
		}
	}
	options := []ftpclient.DialOption{
		ftpclient.DialWithContext(ctx),
		ftpclient.DialWithTimeout(15 * time.Second),
	}
	tlsConfig := ftpTLSConfig(parsed)
	if parsed.Scheme == "ftps" || parsed.Scheme == "ftpes" {
		options = append(options, ftpclient.DialWithExplicitTLS(tlsConfig))
	} else if parsed.Scheme == "ftps+implicit" {
		options = append(options, ftpclient.DialWithTLS(tlsConfig))
	}
	address := net.JoinHostPort(host, port)
	client, err := ftpclient.Dial(address, options...)
	if err != nil {
		return nil, err
	}
	user := "anonymous"
	if parsed.User != nil && parsed.User.Username() != "" {
		user = parsed.User.Username()
	}
	if password == "" && parsed.User != nil {
		password, _ = parsed.User.Password()
	}
	if user == "anonymous" && password == "" {
		password = "anonymous@"
	}
	if err := client.Login(user, password); err != nil {
		client.Quit()
		return nil, err
	}
	label := strings.ToUpper(parsed.Scheme) + " " + user + "@" + address
	return &FTP{client: client, id: parsed.Scheme + "://" + user + "@" + address, label: label}, nil
}

func ftpTLSServerName(parsed *url.URL) string {
	if name := strings.TrimSpace(parsed.Query().Get("tls-server-name")); name != "" {
		return name
	}
	return parsed.Hostname()
}

func ftpTLSConfig(parsed *url.URL) *tls.Config {
	serverName := ftpTLSServerName(parsed)
	config := &tls.Config{ServerName: serverName, MinVersion: tls.VersionTLS12}
	legacyCommonName := ftpLegacyCommonNameEnabled(parsed)
	pinnedFingerprint := strings.TrimSpace(parsed.Query().Get("tls-cert-sha256"))
	if !legacyCommonName && pinnedFingerprint == "" {
		return config
	}
	// Go's default verifier intentionally rejects Common Name-only
	// certificates. Replace it only for an explicitly opted-in connection,
	// while still validating the CA chain, validity period, server-auth usage,
	// and exact legacy hostname below.
	config.InsecureSkipVerify = true
	config.VerifyConnection = func(state tls.ConnectionState) error {
		if pinnedFingerprint != "" {
			return verifyPinnedCertificate(state, serverName, pinnedFingerprint, legacyCommonName)
		}
		return verifyLegacyCommonName(state, serverName, nil)
	}
	return config
}

func ftpLegacyCommonNameEnabled(parsed *url.URL) bool {
	switch strings.ToLower(strings.TrimSpace(parsed.Query().Get("tls-legacy-common-name"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

var subjectAltNameOID = asn1.ObjectIdentifier{2, 5, 29, 17}

func verifyLegacyCommonName(state tls.ConnectionState, serverName string, roots *x509.CertPool) error {
	if len(state.PeerCertificates) == 0 {
		return fmt.Errorf("legacy Common Name verification: server sent no certificate")
	}
	leaf := state.PeerCertificates[0]
	for _, extension := range leaf.Extensions {
		if extension.Id.Equal(subjectAltNameOID) {
			return fmt.Errorf("legacy Common Name verification refused: certificate contains a SAN extension")
		}
	}
	if !matchLegacyCommonName(leaf.Subject.CommonName, serverName) {
		return fmt.Errorf("legacy Common Name %q does not match %q", leaf.Subject.CommonName, serverName)
	}
	intermediates := x509.NewCertPool()
	for _, certificate := range state.PeerCertificates[1:] {
		intermediates.AddCert(certificate)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots: roots, Intermediates: intermediates,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		var unknownAuthority x509.UnknownAuthorityError
		if errors.As(err, &unknownAuthority) {
			return &UntrustedCertificateError{
				CommonName:  leaf.Subject.CommonName,
				Fingerprint: certificateFingerprint(leaf),
				Err:         err,
			}
		}
		return fmt.Errorf("legacy Common Name certificate chain: %w", err)
	}
	return nil
}

func verifyPinnedCertificate(state tls.ConnectionState, serverName, expectedFingerprint string, allowLegacyCommonName bool) error {
	if len(state.PeerCertificates) == 0 {
		return fmt.Errorf("pinned certificate verification: server sent no certificate")
	}
	leaf := state.PeerCertificates[0]
	want, err := normalizeCertificateFingerprint(expectedFingerprint)
	if err != nil {
		return err
	}
	got := certificateFingerprint(leaf)
	if got != want {
		return fmt.Errorf("pinned certificate changed: received SHA-256 %s, expected %s", got, want)
	}
	now := time.Now()
	if now.Before(leaf.NotBefore) || now.After(leaf.NotAfter) {
		return fmt.Errorf("pinned certificate is not valid at %s (valid from %s to %s)", now.Format(time.RFC3339), leaf.NotBefore.Format(time.RFC3339), leaf.NotAfter.Format(time.RFC3339))
	}
	if !certificateAllowsServerAuth(leaf) {
		return fmt.Errorf("pinned certificate is not valid for TLS server authentication")
	}
	if certificateHasSAN(leaf) {
		if err := leaf.VerifyHostname(serverName); err != nil {
			return fmt.Errorf("pinned certificate hostname: %w", err)
		}
		return nil
	}
	if !allowLegacyCommonName {
		return fmt.Errorf("pinned certificate has no SAN; enable legacy Common Name verification for this connection")
	}
	if !matchLegacyCommonName(leaf.Subject.CommonName, serverName) {
		return fmt.Errorf("pinned certificate Common Name %q does not match %q", leaf.Subject.CommonName, serverName)
	}
	return nil
}

func certificateHasSAN(certificate *x509.Certificate) bool {
	for _, extension := range certificate.Extensions {
		if extension.Id.Equal(subjectAltNameOID) {
			return true
		}
	}
	return false
}

func certificateAllowsServerAuth(certificate *x509.Certificate) bool {
	if len(certificate.ExtKeyUsage) == 0 {
		return true
	}
	for _, usage := range certificate.ExtKeyUsage {
		if usage == x509.ExtKeyUsageServerAuth || usage == x509.ExtKeyUsageAny {
			return true
		}
	}
	return false
}

func certificateFingerprint(certificate *x509.Certificate) string {
	digest := sha256.Sum256(certificate.Raw)
	return strings.ToUpper(hex.EncodeToString(digest[:]))
}

func normalizeCertificateFingerprint(value string) (string, error) {
	value = strings.NewReplacer(":", "", " ", "", "-", "").Replace(strings.TrimSpace(value))
	value = strings.ToUpper(value)
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return "", fmt.Errorf("TLS certificate SHA-256 fingerprint must contain 64 hexadecimal characters")
	}
	return value, nil
}

func matchLegacyCommonName(pattern, hostname string) bool {
	pattern = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(pattern), "."))
	hostname = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(hostname), "."))
	if pattern == "" || hostname == "" || net.ParseIP(hostname) != nil {
		return false
	}
	if pattern == hostname {
		return true
	}
	if !strings.HasPrefix(pattern, "*.") {
		return false
	}
	suffix := strings.TrimPrefix(pattern, "*")
	prefix := strings.TrimSuffix(hostname, suffix)
	return prefix != "" && !strings.Contains(prefix, ".") && strings.HasSuffix(hostname, suffix)
}

func (f *FTP) ID() string                { return f.id }
func (f *FTP) Label() string             { return f.label }
func (f *FTP) Clean(value string) string { return pathpkg.Clean("/" + strings.TrimPrefix(value, "/")) }
func (f *FTP) Join(value string, parts ...string) string {
	return pathpkg.Join(append([]string{value}, parts...)...)
}
func (f *FTP) Dir(value string) string  { return pathpkg.Dir(value) }
func (f *FTP) Base(value string) string { return pathpkg.Base(value) }

func (f *FTP) List(_ context.Context, remotePath string) ([]Entry, error) {
	items, err := f.client.List(remotePath)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(items))
	for _, item := range items {
		if item.Name == "." || item.Name == ".." {
			continue
		}
		entries = append(entries, Entry{
			Name: item.Name, Path: f.Join(remotePath, item.Name), Size: int64(item.Size),
			ModTime: item.Time, Dir: item.Type == ftpclient.EntryTypeFolder,
			Link: item.Type == ftpclient.EntryTypeLink,
		})
	}
	SortEntries(entries)
	return entries, nil
}

func (f *FTP) Stat(ctx context.Context, remotePath string) (Entry, error) {
	parent := f.Dir(remotePath)
	entries, err := f.List(ctx, parent)
	if err != nil {
		return Entry{}, err
	}
	name := f.Base(remotePath)
	for _, entry := range entries {
		if entry.Name == name {
			return entry, nil
		}
	}
	return Entry{}, fs.ErrNotExist
}

func (f *FTP) Download(ctx context.Context, remotePath string, destination io.Writer, progress func(int64)) error {
	response, err := f.client.Retr(remotePath)
	if err != nil {
		return err
	}
	return copyFTPResponse(ctx, response, destination, progress)
}

func copyFTPResponse(ctx context.Context, response io.ReadCloser, destination io.Writer, progress func(int64)) error {
	_, copyErr := io.Copy(destination, ContextReader(ctx, response, progress))
	closeErr := response.Close()
	return errors.Join(copyErr, closeErr)
}

func (f *FTP) Upload(ctx context.Context, remotePath string, source io.Reader, _ fs.FileMode, progress func(int64)) error {
	return f.client.Stor(remotePath, ContextReader(ctx, source, progress))
}

func (f *FTP) Mkdir(_ context.Context, remotePath string, _ fs.FileMode) error {
	if remotePath == "/" || remotePath == "." {
		return nil
	}
	if err := f.client.MakeDir(remotePath); err != nil {
		// Copying a directory into an existing tree is a merge operation. FTP
		// servers commonly report "already exists" with an implementation-
		// specific status, so verify the directory instead of matching text.
		if _, listErr := f.client.List(remotePath); listErr == nil {
			return nil
		}
		return err
	}
	return nil
}

func (f *FTP) Remove(ctx context.Context, remotePath string, recursive bool) error {
	if recursive {
		children, err := f.List(ctx, remotePath)
		if err != nil {
			return err
		}
		for _, child := range children {
			if err := f.Remove(ctx, child.Path, child.Dir); err != nil {
				return err
			}
		}
		return f.client.RemoveDir(remotePath)
	}
	return f.client.Delete(remotePath)
}

func (f *FTP) Rename(_ context.Context, oldPath, newPath string) error {
	return f.client.Rename(oldPath, newPath)
}

func (f *FTP) Close() error { return f.client.Quit() }
