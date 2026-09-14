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
	"strings"
	"time"

	smb2 "github.com/cloudsoda/go-smb2"
)

type SMB struct {
	session *smb2.Session
	share   *smb2.Share
	root    string
	id      string
	label   string
}

type smbLocation struct {
	host, port, user, password, domain, share string
}

func parseSMBLocation(parsed *url.URL, password string) (smbLocation, error) {
	location := smbLocation{host: parsed.Hostname(), port: parsed.Port(), password: password}
	if location.host == "" {
		return location, fmt.Errorf("SMB location has no host")
	}
	if location.port == "" {
		location.port = "445"
	}
	if parsed.User != nil {
		location.user = parsed.User.Username()
		if location.password == "" {
			location.password, _ = parsed.User.Password()
		}
	}
	location.domain = parsed.Query().Get("domain")
	if separator := strings.IndexAny(location.user, `;\`); separator >= 0 {
		if location.domain == "" {
			location.domain = location.user[:separator]
		}
		location.user = location.user[separator+1:]
	}
	if location.user == "" {
		location.user = "guest"
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return location, fmt.Errorf("SMB location must include a share: smb://user@host/share/path")
	}
	location.share = parts[0]
	return location, nil
}

func NewSMB(ctx context.Context, parsed *url.URL, password string) (*SMB, error) {
	location, err := parseSMBLocation(parsed, password)
	if err != nil {
		return nil, err
	}
	address := net.JoinHostPort(location.host, location.port)
	connection, err := (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	dialer := &smb2.Dialer{
		Negotiator: smb2.Negotiator{RequireMessageSigning: true},
		Initiator: &smb2.NTLMInitiator{
			User: location.user, Password: location.password, Domain: location.domain,
		},
	}
	session, err := dialer.DialConn(ctx, connection, location.host)
	if err != nil {
		connection.Close()
		return nil, err
	}
	share, err := session.WithContext(ctx).Mount(location.share)
	if err != nil {
		session.Logoff()
		return nil, err
	}
	identity := location.user
	if location.domain != "" {
		identity = location.domain + `\` + identity
	}
	root := "/" + location.share
	return &SMB{
		session: session,
		share:   share,
		root:    root,
		id:      "smb://" + identity + "@" + address + root,
		label:   "SMB " + identity + "@" + address + root,
	}, nil
}

func (s *SMB) ID() string    { return s.id }
func (s *SMB) Label() string { return s.label }

func (s *SMB) Clean(value string) string {
	cleaned := pathpkg.Clean("/" + strings.TrimPrefix(value, "/"))
	if cleaned == "/" {
		return s.root
	}
	if cleaned == s.root || strings.HasPrefix(cleaned, s.root+"/") {
		return cleaned
	}
	return pathpkg.Join(s.root, cleaned)
}

func (s *SMB) Join(value string, parts ...string) string {
	return s.Clean(pathpkg.Join(append([]string{value}, parts...)...))
}

func (s *SMB) Dir(value string) string {
	value = s.Clean(value)
	if value == s.root {
		return s.root
	}
	return pathpkg.Dir(value)
}

func (s *SMB) Base(value string) string { return pathpkg.Base(s.Clean(value)) }

func (s *SMB) sharePath(value string) string {
	relative := strings.TrimPrefix(s.Clean(value), s.root)
	relative = strings.TrimPrefix(relative, "/")
	if relative == "" {
		return "."
	}
	return relative
}

func (s *SMB) List(ctx context.Context, remotePath string) ([]Entry, error) {
	items, err := s.share.WithContext(ctx).ReadDir(s.sharePath(remotePath))
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
		if item.Name() == "." || item.Name() == ".." {
			continue
		}
		entries = append(entries, Entry{
			Name: item.Name(), Path: s.Join(remotePath, item.Name()), Size: item.Size(),
			Mode: item.Mode(), ModTime: item.ModTime(), Dir: item.IsDir(), Link: item.Mode()&os.ModeSymlink != 0,
		})
	}
	SortEntries(entries)
	return entries, nil
}

func (s *SMB) Stat(ctx context.Context, remotePath string) (Entry, error) {
	item, err := s.share.WithContext(ctx).Stat(s.sharePath(remotePath))
	if err != nil {
		return Entry{}, err
	}
	return Entry{
		Name: item.Name(), Path: s.Clean(remotePath), Size: item.Size(), Mode: item.Mode(),
		ModTime: item.ModTime(), Dir: item.IsDir(), Link: item.Mode()&os.ModeSymlink != 0,
	}, nil
}

func (s *SMB) Download(ctx context.Context, remotePath string, destination io.Writer, progress func(int64)) error {
	file, err := s.share.WithContext(ctx).Open(s.sharePath(remotePath))
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(destination, ContextReader(ctx, file, progress))
	return err
}

func (s *SMB) Upload(ctx context.Context, remotePath string, source io.Reader, mode fs.FileMode, progress func(int64)) error {
	share := s.share.WithContext(ctx)
	sharePath := s.sharePath(remotePath)
	if err := share.MkdirAll(pathpkg.Dir(sharePath), 0o755); err != nil {
		return err
	}
	file, err := share.OpenFile(sharePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, ModeForUpload(mode))
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, ContextReader(ctx, source, progress))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func (s *SMB) Mkdir(ctx context.Context, remotePath string, mode fs.FileMode) error {
	if s.Clean(remotePath) == s.root {
		return nil
	}
	if mode.Perm() == 0 {
		mode = 0o755
	}
	return s.share.WithContext(ctx).MkdirAll(s.sharePath(remotePath), mode.Perm())
}

func (s *SMB) Remove(ctx context.Context, remotePath string, recursive bool) error {
	if s.Clean(remotePath) == s.root {
		return fmt.Errorf("cannot remove the root of an SMB share")
	}
	share := s.share.WithContext(ctx)
	if recursive {
		return share.RemoveAll(s.sharePath(remotePath))
	}
	return share.Remove(s.sharePath(remotePath))
}

func (s *SMB) Rename(ctx context.Context, oldPath, newPath string) error {
	if s.Clean(oldPath) == s.root {
		return fmt.Errorf("cannot rename the root of an SMB share")
	}
	return s.share.WithContext(ctx).Rename(s.sharePath(oldPath), s.sharePath(newPath))
}

func (s *SMB) Close() error {
	return errors.Join(s.share.Umount(), s.session.Logoff())
}
