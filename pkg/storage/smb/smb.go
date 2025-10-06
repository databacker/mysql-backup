package smb

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudsoda/go-smb2"
	log "github.com/sirupsen/logrus"
)

const (
	defaultSMBPort = "445"
)

type SMB struct {
	url      url.URL
	domain   string
	username string
	password string
}

type Option func(s *SMB)

func WithDomain(domain string) Option {
	return func(s *SMB) {
		s.domain = domain
	}
}
func WithUsername(username string) Option {
	return func(s *SMB) {
		s.username = username
	}
}
func WithPassword(password string) Option {
	return func(s *SMB) {
		s.password = password
	}
}

func New(u url.URL, opts ...Option) *SMB {
	s := &SMB{url: u}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *SMB) Pull(ctx context.Context, source, target string, logger *log.Entry) (int64, error) {
	var (
		copied int64
		err    error
	)
	err = s.exec(s.url, func(fs *smb2.Share, sharepath string) error {
		smbFilename := fmt.Sprintf("%s%c%s", sharepath, smb2.PathSeparator, filepath.Base(strings.ReplaceAll(target, ":", "-")))
		smbFilename = strings.TrimPrefix(smbFilename, fmt.Sprintf("%c", smb2.PathSeparator))

		sourceFile := smbFilename
		u, err := url.Parse(smbFilename)
		if err != nil {
			return fmt.Errorf("failed to parse target URL %s: %v", source, err)
		}
		q := u.Query()
		if q.Has("latest") {
			latestFilename, err := s.Latest(ctx, u.Path, logger)
			if err != nil {
				return fmt.Errorf("failed to find latest file for target %s: %v", u.Path, err)
			}
			logger.Debugf("latest file for target %s is %s", u.Path, latestFilename)
			sourceFile = filepath.Join(u.Path, latestFilename)
		}

		to, err := os.Create(target)
		if err != nil {
			return err
		}
		defer func() { _ = to.Close() }()
		from, err := fs.Open(sourceFile)
		if err != nil {
			return err
		}
		defer func() { _ = from.Close() }()
		copied, err = io.Copy(to, from)
		return err
	})
	return copied, err
}

func (s *SMB) Latest(ctx context.Context, target string, logger *log.Entry) (string, error) {
	var (
		latest string
		err    error
	)
	err = s.exec(s.url, func(fs *smb2.Share, sharepath string) error {
		smbDirname := fmt.Sprintf("%s%c%s", sharepath, smb2.PathSeparator, target)
		smbDirname = strings.TrimPrefix(smbDirname, fmt.Sprintf("%c", smb2.PathSeparator))
		entries, err := fs.ReadDir(smbDirname)
		if err != nil {
			return fmt.Errorf("failed to read directory %s: %w", smbDirname, err)
		}

		var latestModTime int64

		for _, entry := range entries {
			if entry.IsDir() || !entry.Mode().IsRegular() {
				continue
			}

			if entry.ModTime().Unix() > latestModTime {
				latest = entry.Name()
				latestModTime = entry.ModTime().Unix()
			}
		}

		if latest == "" {
			return fmt.Errorf("no files found for target %s", target)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return latest, nil
}

func (s *SMB) Push(ctx context.Context, target, source string, logger *log.Entry) (int64, error) {
	var (
		copied int64
		err    error
	)
	err = s.exec(s.url, func(fs *smb2.Share, sharepath string) error {
		smbFilename := fmt.Sprintf("%s%c%s", sharepath, smb2.PathSeparator, target)
		smbFilename = strings.TrimPrefix(smbFilename, fmt.Sprintf("%c", smb2.PathSeparator))
		from, err := os.Open(source)
		if err != nil {
			return err
		}
		defer func() { _ = from.Close() }()
		to, err := fs.Create(smbFilename)
		if err != nil {
			return err
		}
		defer func() { _ = to.Close() }()
		copied, err = io.Copy(to, from)
		return err
	})
	return copied, err
}

func (s *SMB) Clean(filename string) string {
	return strings.ReplaceAll(filename, ":", "-")
}

func (s *SMB) Protocol() string {
	return "smb"
}

func (s *SMB) URL() string {
	return s.url.String()
}

func (s *SMB) ReadDir(ctx context.Context, dirname string, logger *log.Entry) ([]os.FileInfo, error) {
	var (
		err   error
		infos []os.FileInfo
	)
	err = s.exec(s.url, func(fs *smb2.Share, sharepath string) error {
		infos, err = fs.ReadDir(sharepath)
		return err
	})
	return infos, err
}

func (s *SMB) Remove(ctx context.Context, target string, logger *log.Entry) error {
	return s.exec(s.url, func(fs *smb2.Share, sharepath string) error {
		smbFilename := fmt.Sprintf("%s%c%s", sharepath, smb2.PathSeparator, filepath.Base(strings.ReplaceAll(target, ":", "-")))
		smbFilename = strings.TrimPrefix(smbFilename, fmt.Sprintf("%c", smb2.PathSeparator))
		return fs.Remove(smbFilename)
	})
}

func (s *SMB) exec(u url.URL, command func(fs *smb2.Share, sharepath string) error) error {
	var (
		username = s.username
		password = s.password
		domain   = s.domain
	)

	hostname, port, path := u.Hostname(), u.Port(), u.Path
	// set default port
	if port == "" {
		port = defaultSMBPort
	}
	host := fmt.Sprintf("%s:%s", hostname, port)
	share, sharepath := parseSMBPath(path)
	if s.username == "" && u.User != nil {
		username = u.User.Username()
		password, _ = u.User.Password()
		username, domain = parseSMBDomain(username)
	}

	conn, err := net.Dial("tcp", host)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	d := &smb2.Dialer{
		Initiator: &smb2.NTLMInitiator{
			Domain:   domain,
			User:     username,
			Password: password,
		},
	}

	smbConn, err := d.Dial(conn)
	if err != nil {
		return err
	}
	defer func() {
		_ = smbConn.Logoff()
	}()

	fs, err := smbConn.Mount(share)
	if err != nil {
		return err
	}
	defer func() {
		_ = fs.Umount()
	}()
	return command(fs, sharepath)
}

// parseSMBDomain parse a username to get an SMB domain
// nolint: unused
func parseSMBDomain(username string) (user, domain string) {
	parts := strings.SplitN(username, ";", 2)
	if len(parts) < 2 {
		return username, ""
	}
	// if we reached this point, we have a username that has a domain in it
	return parts[1], parts[0]
}

// parseSMBPath parse an smb path into its constituent parts
func parseSMBPath(path string) (share, sharepath string) {
	sep := "/"
	parts := strings.Split(path, sep)
	if len(parts) <= 1 {
		return path, ""
	}
	// if the path started with a slash, it might have an empty string as the first element
	if parts[0] == "" {
		parts = parts[1:]
	}
	// ensure no leading / as it messes up SMB
	return parts[0], strings.Join(parts[1:], sep)
}
