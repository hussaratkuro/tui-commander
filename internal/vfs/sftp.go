package vfs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

type SFTP struct {
	client    *sftp.Client
	sshClient *ssh.Client
	agentConn net.Conn
	id        string
	label     string
}

func NewSFTP(ctx context.Context, parsed *url.URL, password string) (*SFTP, error) {
	host := parsed.Hostname()
	if host == "" {
		return nil, fmt.Errorf("SFTP location has no host")
	}
	port := parsed.Port()
	if port == "" {
		port = "22"
	}
	address := net.JoinHostPort(host, port)
	user := ""
	if parsed.User != nil {
		user = parsed.User.Username()
	}
	if user == "" {
		user = os.Getenv("USER")
	}
	if password == "" && parsed.User != nil {
		password, _ = parsed.User.Password()
	}

	auth, agentConn := sshAuthMethods(password)
	knownHosts, err := knownHostsCallback()
	if err != nil {
		if agentConn != nil {
			agentConn.Close()
		}
		return nil, err
	}
	sshConfig := &ssh.ClientConfig{
		User: user, Auth: auth, HostKeyCallback: knownHosts, Timeout: 15 * time.Second,
	}
	dialer := net.Dialer{Timeout: 15 * time.Second}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		if agentConn != nil {
			agentConn.Close()
		}
		return nil, err
	}
	clientConn, channels, requests, err := ssh.NewClientConn(connection, address, sshConfig)
	if err != nil {
		connection.Close()
		if agentConn != nil {
			agentConn.Close()
		}
		return nil, err
	}
	sshClient := ssh.NewClient(clientConn, channels, requests)
	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		sshClient.Close()
		if agentConn != nil {
			agentConn.Close()
		}
		return nil, err
	}
	return &SFTP{
		client: sftpClient, sshClient: sshClient, agentConn: agentConn,
		id: "sftp://" + user + "@" + address, label: "SFTP " + user + "@" + address,
	}, nil
}

func sshAuthMethods(password string) ([]ssh.AuthMethod, net.Conn) {
	var methods []ssh.AuthMethod
	if password != "" {
		methods = append(methods, ssh.Password(password), ssh.KeyboardInteractive(func(_, _ string, questions []string, echoes []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i := range answers {
				answers[i] = password
			}
			return answers, nil
		}))
	}
	var agentConn net.Conn
	if socket := os.Getenv("SSH_AUTH_SOCK"); socket != "" {
		if connection, err := net.Dial("unix", socket); err == nil {
			agentConn = connection
			methods = append(methods, ssh.PublicKeysCallback(agent.NewClient(connection).Signers))
		}
	}
	home, _ := os.UserHomeDir()
	for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
		data, err := os.ReadFile(filepath.Join(home, ".ssh", name))
		if err != nil {
			continue
		}
		signer, err := ssh.ParsePrivateKey(data)
		if err != nil && password != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(data, []byte(password))
		}
		if err == nil {
			methods = append(methods, ssh.PublicKeys(signer))
		}
	}
	return methods, agentConn
}

func knownHostsCallback() (ssh.HostKeyCallback, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, ".ssh", "known_hosts")
	callback, err := knownhosts.New(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s does not exist; connect once with ssh to verify and save the host key", path)
		}
		return nil, err
	}
	return callback, nil
}

func (s *SFTP) ID() string                { return s.id }
func (s *SFTP) Label() string             { return s.label }
func (s *SFTP) Clean(value string) string { return pathpkg.Clean("/" + strings.TrimPrefix(value, "/")) }
func (s *SFTP) Join(value string, parts ...string) string {
	return pathpkg.Join(append([]string{value}, parts...)...)
}
func (s *SFTP) Dir(value string) string  { return pathpkg.Dir(value) }
func (s *SFTP) Base(value string) string { return pathpkg.Base(value) }

func (s *SFTP) List(ctx context.Context, remotePath string) ([]Entry, error) {
	items, err := s.client.ReadDir(remotePath)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(items))
	for _, item := range items {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		entries = append(entries, Entry{
			Name: item.Name(), Path: s.Join(remotePath, item.Name()), Size: item.Size(),
			Mode: item.Mode(), ModTime: item.ModTime(), Dir: item.IsDir(), Link: item.Mode()&os.ModeSymlink != 0,
		})
	}
	SortEntries(entries)
	return entries, nil
}

func (s *SFTP) Stat(_ context.Context, remotePath string) (Entry, error) {
	item, err := s.client.Stat(remotePath)
	if err != nil {
		return Entry{}, err
	}
	return Entry{Name: item.Name(), Path: remotePath, Size: item.Size(), Mode: item.Mode(), ModTime: item.ModTime(), Dir: item.IsDir()}, nil
}

func (s *SFTP) Download(ctx context.Context, remotePath string, destination io.Writer, progress func(int64)) error {
	file, err := s.client.Open(remotePath)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(destination, ContextReader(ctx, file, progress))
	return err
}

func (s *SFTP) Upload(ctx context.Context, remotePath string, source io.Reader, mode fs.FileMode, progress func(int64)) error {
	if err := s.client.MkdirAll(pathpkg.Dir(remotePath)); err != nil {
		return err
	}
	file, err := s.client.OpenFile(remotePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, ContextReader(ctx, source, progress))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if mode.Perm() != 0 {
		return s.client.Chmod(remotePath, mode.Perm())
	}
	return nil
}

func (s *SFTP) Mkdir(_ context.Context, remotePath string, _ fs.FileMode) error {
	return s.client.MkdirAll(remotePath)
}

func (s *SFTP) Remove(ctx context.Context, remotePath string, recursive bool) error {
	if recursive {
		children, err := s.List(ctx, remotePath)
		if err != nil {
			return err
		}
		for _, child := range children {
			if err := s.Remove(ctx, child.Path, child.Dir); err != nil {
				return err
			}
		}
		return s.client.RemoveDirectory(remotePath)
	}
	return s.client.Remove(remotePath)
}

func (s *SFTP) Rename(_ context.Context, oldPath, newPath string) error {
	return s.client.Rename(oldPath, newPath)
}

func (s *SFTP) Close() error {
	clientErr := s.client.Close()
	sshErr := s.sshClient.Close()
	if s.agentConn != nil {
		s.agentConn.Close()
	}
	if clientErr != nil {
		return clientErr
	}
	return sshErr
}
