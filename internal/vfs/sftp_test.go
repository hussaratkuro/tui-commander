package vfs

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestSFTPBackendRoundTrip(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "remote.txt"), []byte("from server"), 0o644); err != nil {
		t.Fatal(err)
	}
	address, hostKey, stop := startTestSFTPServer(t)
	defer stop()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	knownHostsLine := knownhosts.Line([]string{address}, hostKey.PublicKey()) + "\n"
	if err := os.WriteFile(filepath.Join(home, ".ssh", "known_hosts"), []byte(knownHostsLine), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	parsed, err := url.Parse("sftp://tester@" + address + filepath.ToSlash(root))
	if err != nil {
		t.Fatal(err)
	}
	backend, err := NewSFTP(context.Background(), parsed, "password")
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	entries, err := backend.List(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "remote.txt" {
		t.Fatalf("entries = %#v", entries)
	}
	var downloaded []byte
	writer := byteWriter{write: func(data []byte) { downloaded = append(downloaded, data...) }}
	if err := backend.Download(context.Background(), entries[0].Path, writer, nil); err != nil {
		t.Fatal(err)
	}
	if string(downloaded) != "from server" {
		t.Fatalf("downloaded = %q", downloaded)
	}
	uploadPath := filepath.ToSlash(filepath.Join(root, "uploaded.txt"))
	if err := backend.Upload(context.Background(), uploadPath, &byteReader{data: []byte("to server")}, 0o640, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "uploaded.txt"))
	if err != nil || string(data) != "to server" {
		t.Fatalf("uploaded data = %q, err = %v", data, err)
	}
}

type byteWriter struct{ write func([]byte) }

func (w byteWriter) Write(data []byte) (int, error) {
	w.write(data)
	return len(data), nil
}

type byteReader struct{ data []byte }

func (r *byteReader) Read(buffer []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := copy(buffer, r.data)
	r.data = r.data[n:]
	return n, nil
}

func startTestSFTPServer(t *testing.T) (string, ssh.Signer, func()) {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostKey, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	serverConfig := &ssh.ServerConfig{
		PasswordCallback: func(metadata ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if metadata.User() == "tester" && string(password) == "password" {
				return nil, nil
			}
			return nil, os.ErrPermission
		},
	}
	serverConfig.AddHostKey(hostKey)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		if errors.Is(err, syscall.EPERM) {
			t.Skip("sandbox does not permit a loopback integration server")
		}
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		sshConnection, channels, requests, err := ssh.NewServerConn(connection, serverConfig)
		if err != nil {
			connection.Close()
			return
		}
		defer sshConnection.Close()
		go ssh.DiscardRequests(requests)
		for channelRequest := range channels {
			if channelRequest.ChannelType() != "session" {
				channelRequest.Reject(ssh.UnknownChannelType, "session required")
				continue
			}
			channel, channelRequests, err := channelRequest.Accept()
			if err != nil {
				continue
			}
			go func() {
				defer channel.Close()
				for request := range channelRequests {
					accepted := request.Type == "subsystem" && subsystemName(request.Payload) == "sftp"
					request.Reply(accepted, nil)
					if accepted {
						server, err := sftp.NewServer(channel)
						if err == nil {
							_ = server.Serve()
							_ = server.Close()
						}
						return
					}
				}
			}()
		}
	}()
	stop := func() {
		listener.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}
	return listener.Addr().String(), hostKey, stop
}

func subsystemName(payload []byte) string {
	if len(payload) < 4 {
		return ""
	}
	length := int(binary.BigEndian.Uint32(payload[:4]))
	if length < 0 || len(payload) < 4+length {
		return ""
	}
	return string(payload[4 : 4+length])
}
