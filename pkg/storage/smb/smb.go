package smb

import (
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudsoda/go-smb2"
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

func (s *SMB) Pull(source, target string) (int64, error) {
	return s.command(false, s.url, source, target)
}

func (s *SMB) Push(target, source string) (int64, error) {
	return s.command(true, s.url, target, source)
}

func (s *SMB) Protocol() string {
	return "smb"
}

func (s *SMB) URL() string {
	return s.url.String()
}

func (s *SMB) command(push bool, u url.URL, remoteFilename, filename string) (int64, error) {
	var (
		username, password, domain string
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
	}

	username, domain = parseSMBDomain(username)

	conn, err := net.Dial("tcp", host)
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	d := &smb2.Dialer{
		Initiator: &smb2.NTLMInitiator{
			Domain:   domain,
			User:     username,
			Password: password,
		},
	}

	smbConn, err := d.Dial(conn)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = smbConn.Logoff()
	}()

	fs, err := smbConn.Mount(share)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = fs.Umount()
	}()

	smbFilename := fmt.Sprintf("%s%c%s", sharepath, smb2.PathSeparator, filepath.Base(strings.ReplaceAll(remoteFilename, ":", "-")))

	var (
		from io.ReadCloser
		to   io.WriteCloser
	)
	if push {
		from, err = os.Open(filename)
		if err != nil {
			return 0, err
		}
		defer from.Close()
		to, err = fs.Create(smbFilename)
		if err != nil {
			return 0, err
		}
		defer to.Close()
	} else {
		to, err = os.Create(filename)
		if err != nil {
			return 0, err
		}
		defer to.Close()
		from, err = fs.Open(smbFilename)
		if err != nil {
			return 0, err
		}
		defer from.Close()
	}
	return io.Copy(to, from)
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
