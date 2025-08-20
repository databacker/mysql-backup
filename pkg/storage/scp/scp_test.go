package scp

import (
	"bufio"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	gliderssh "github.com/gliderlabs/ssh"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const (
	testUser = "alice"
	testPass = "secret"
)

func TestPull(t *testing.T) {
	const (
		aFile = "a.txt"
		bFile = "b.txt"
	)
	files := map[string][]byte{
		aFile: []byte("hello"),
		bFile: []byte("world"),
	}
	// start the server
	server := testStartServerWithKeys(t)

	// Prepare some files in the server's root dir
	for f, content := range files {
		if err := os.WriteFile(filepath.Join(server.RootDir, f), content, 0600); err != nil {
			t.Fatalf("failed to write file %s: %v", f, err)
		}
	}

	// check the files
	handler := New(url.URL{Scheme: "scp", Host: server.Addr})
	tmpDir := t.TempDir()

	for f, content := range files {
		n, err := handler.Pull(context.Background(), f, filepath.Join(tmpDir, f), nil)
		if err != nil {
			t.Fatalf("failed to pull file %s: %v", f, err)
		}
		if n != int64(len(content)) {
			t.Errorf("pulled %d bytes instead of expected %d", n, len(content))
		}
		localContent, err := os.ReadFile(filepath.Join(tmpDir, f))
		if err != nil {
			t.Fatalf("failed to read local file %s: %v", f, err)
		}
		if string(localContent) != string(content) {
			t.Errorf("local file %s content mismatch: got %q, want %q", f, localContent, content)
		}
	}
}

func TestPush(t *testing.T) {
	const (
		aFile = "a.txt"
		bFile = "b.txt"
	)
	files := map[string][]byte{
		aFile: []byte("hello"),
		bFile: []byte("world"),
	}
	// start the server
	server := testStartServerWithKeys(t)

	// Prepare some files in the sethe localrver's root dir
	tmpDir := t.TempDir()
	for f, content := range files {
		_ = os.WriteFile(filepath.Join(tmpDir, f), content, 0600)
	}

	// check the files
	handler := New(url.URL{Scheme: "scp", Host: server.Addr})

	for f, content := range files {
		localFile := filepath.Join(tmpDir, f)
		n, err := handler.Push(context.Background(), f, localFile, nil)
		if err != nil {
			t.Fatalf("failed to push file %s: %v", localFile, err)
		}
		if n != int64(len(content)) {
			t.Errorf("pushed %d bytes instead of expected %d", n, len(content))
		}
		serverFile := filepath.Join(server.RootDir, f)
		foundContent, err := os.ReadFile(serverFile)
		if err != nil {
			t.Fatalf("failed to read server file %s: %v", serverFile, err)
		}
		if string(foundContent) != string(content) {
			t.Errorf("server file %s content mismatch: got %q, want %q", f, foundContent, content)
		}
	}
}

func TestReadDir(t *testing.T) {
	const (
		aFile = "a.txt"
		bFile = "b.txt"
	)
	files := map[string][]byte{
		aFile: []byte("hello"),
		bFile: []byte("world"),
	}
	// start the server
	server := testStartServerWithKeys(t)

	// Prepare some files in the server's root dir
	for f, content := range files {
		_ = os.WriteFile(filepath.Join(server.RootDir, f), content, 0600)
	}

	// check the files
	handler := New(url.URL{Scheme: "scp", Host: server.Addr})
	// sftp would require a lot of work on our end to make it chrooted, so we are not bothering
	fileInfo, err := handler.ReadDir(context.Background(), server.RootDir, nil)
	if err != nil {
		t.Fatalf("failed to read remote directory: %v", err)
	}
	if len(fileInfo) != len(files) {
		t.Errorf("unexpected number of files: got %d, want %d", len(fileInfo), len(files))
	}
	// sort and compare
	var filenames []string
	for f := range files {
		filenames = append(filenames, f)
	}
	sort.Strings(filenames)
	sort.Slice(fileInfo, func(i, j int) bool {
		return fileInfo[i].Name() < fileInfo[j].Name()
	})
	for i, fi := range fileInfo {
		if fi.Name() != filenames[i] {
			t.Errorf("file %d: got %s, want %s", i, fi.Name(), filenames[i])
		}
	}
}

func TestRemove(t *testing.T) {
	const (
		aFile = "a.txt"
		bFile = "b.txt"
	)
	files := map[string][]byte{
		aFile: []byte("hello"),
		bFile: []byte("world"),
	}
	// start the server
	server := testStartServerWithKeys(t)

	// Prepare some files in the server's root dir
	for f, content := range files {
		_ = os.WriteFile(filepath.Join(server.RootDir, f), content, 0600)
	}

	// check the files
	handler := New(url.URL{Scheme: "scp", Host: server.Addr})
	err := handler.Remove(context.Background(), aFile, nil)
	if err != nil {
		t.Fatalf("failed to remove file %s: %v", aFile, err)
	}
	// see that it no longer exists
	_, err = os.Stat(filepath.Join(server.RootDir, aFile))
	if err == nil {
		t.Fatalf("file %s still exists after removal", aFile)
	}
}

func TestConnection(t *testing.T) {
	t.Run("no keyfile found", func(t *testing.T) {
		server := testStartServer(t)
		tmpDir := t.TempDir()
		if err := os.Setenv("SSH_HOME", tmpDir); err != nil {
			t.Fatalf("failed to set SSH_HOME: %v", err)
		}
		t.Cleanup(func() { _ = os.Unsetenv("SSH_HOME") })

		handler := New(url.URL{Scheme: "scp", Host: server.Addr})
		_, err := handler.getSSHClient()
		if err == nil {
			t.Fatal("expected error getting SSH client, got none")
		}
		t.Logf("%+v", err)
	})
	t.Run("invalid keyfile found", func(t *testing.T) {
		server := testStartServer(t)
		tmpDir := t.TempDir()
		if err := os.Setenv("SSH_HOME", tmpDir); err != nil {
			t.Fatalf("failed to set SSH_HOME: %v", err)
		}
		t.Cleanup(func() { _ = os.Unsetenv("SSH_HOME") })

		handler := New(url.URL{Scheme: "scp", Host: server.Addr})
		_, err := handler.getSSHClient()
		if err == nil {
			t.Fatal("expected error getting SSH client, got none")
		}
		t.Logf("%+v", err)
	})
	t.Run("valid keyfile found in directory", func(t *testing.T) {
		server := testStartServer(t)
		tmpDir := t.TempDir()
		if err := os.Setenv("SSH_HOME", tmpDir); err != nil {
			t.Fatalf("failed to set SSH_HOME: %v", err)
		}
		t.Cleanup(func() { _ = os.Unsetenv("SSH_HOME") })

		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("generate keypair: %v", err)
		}
		if err := testWriteKeypairToDir(pub, priv, tmpDir); err != nil {
			t.Fatalf("write keypair: %v", err)
		}
		sshPub, err := ssh.NewPublicKey(pub)
		if err != nil {
			t.Fatalf("failed to create SSH public key: %v", err)
		}
		server.UserKeys = append(server.UserKeys, sshPub)

		handler := New(url.URL{Scheme: "scp", Host: server.Addr})
		_, err = handler.getSSHClient()
		if err != nil {
			t.Fatalf("unexpected error getting SSH client: %v", err)
		}
	})
	t.Run("explicitly specified in ssh_config", func(t *testing.T) {
		server := testStartServer(t)
		tmpDir := t.TempDir()
		if err := os.Setenv("SSH_HOME", tmpDir); err != nil {
			t.Fatalf("failed to set SSH_HOME: %v", err)
		}
		t.Cleanup(func() { _ = os.Unsetenv("SSH_HOME") })

		// create ssh config
		configFilename := filepath.Join(tmpDir, "config")
		keyFilename := filepath.Join(tmpDir, "unique")
		config := fmt.Sprintf(`
Host testhost
  HostName %s
  Port %d
  IdentityFile %s
`, server.Hostname(), server.Port(), keyFilename)
		_ = os.WriteFile(configFilename, []byte(config), 0600)

		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("generate keypair: %v", err)
		}
		if err := testWriteKeypairToDir(pub, priv, tmpDir); err != nil {
			t.Fatalf("write keypair: %v", err)
		}
		sshPub, err := ssh.NewPublicKey(pub)
		if err != nil {
			t.Fatalf("failed to create SSH public key: %v", err)
		}
		server.UserKeys = append(server.UserKeys, sshPub)

		handler := New(url.URL{Scheme: "scp", Host: server.Addr})
		_, err = handler.getSSHClient()
		if err != nil {
			t.Fatalf("unexpected error getting SSH client: %v", err)
		}
	})
}

func testWriteKeypairToDir(pubkey crypto.PublicKey, privkey crypto.PrivateKey, dir string) error {
	privBytes, err := x509.MarshalPKCS8PrivateKey(privkey)
	if err != nil {
		return fmt.Errorf("marshal priv: %w", err)
	}
	privPem := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privBytes,
	})

	pubKey, err := ssh.NewPublicKey(pubkey)
	if err != nil {
		return fmt.Errorf("marshal pub: %w", err)
	}
	pubBytes := ssh.MarshalAuthorizedKey(pubKey)

	// 4. Write files
	privPath := filepath.Join(dir, "id_ed25519")
	pubPath := filepath.Join(dir, "id_ed25519.pub")

	if err := os.WriteFile(privPath, privPem, 0600); err != nil {
		return fmt.Errorf("write priv: %w", err)
	}
	if err := os.WriteFile(pubPath, pubBytes, 0644); err != nil {
		return fmt.Errorf("write pub: %w", err)
	}

	return nil
}

// testStartServer starts a server
func testStartServer(t *testing.T) *Server {
	t.Helper()
	root := t.TempDir()
	// Generate an ECDSA host key (ephemeral, per test)
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("host key: %v", err)
	}

	s := &Server{
		User:     testUser,
		Password: testPass,
		Key:      signer,
		RootDir:  root,
	}
	pwAuth := func(ctx gliderssh.Context, password string) bool {
		return ctx.User() == testUser && password == testPass
	}
	pubAuth := func(ctx gliderssh.Context, key gliderssh.PublicKey) bool {
		for _, ak := range s.UserKeys {
			if ssh.FingerprintSHA256(ak) == ssh.FingerprintSHA256(key) {
				return true
			}
		}
		return false
	}

	// SFTP subsystem handler
	subsystemHandlers := map[string]gliderssh.SubsystemHandler{
		"sftp": func(sess gliderssh.Session) {
			// Start an sftp server bound to the session
			s, err := sftp.NewServer(sess, sftp.WithDebug(nil), sftp.WithServerWorkingDirectory(root))
			if err != nil {
				_, _ = io.WriteString(sess, "sftp start error: "+err.Error())
				return
			}

			defer func() { _ = s.Close() }()
			_ = s.Serve() // blocks until client closes
		},
	}

	handler := scpExecHandler(s.RootDir)
	server := &gliderssh.Server{
		HostSigners:       []gliderssh.Signer{s.Key},
		Addr:              "127.0.0.1:0",
		PasswordHandler:   pwAuth,
		PublicKeyHandler:  pubAuth,
		SubsystemHandlers: subsystemHandlers,
		Handler:           handler,
	}
	// Bind
	lis, err := net.Listen("tcp", server.Addr)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s.lis = lis
	s.Addr = lis.Addr().String()
	s.srv = server

	go func() {
		_ = server.Serve(lis) // stops when Close() is called
	}()
	// Wait for it to be ready
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("tcp", s.Addr)
		if err == nil {
			_ = conn.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	return s
}

// testStartServerWithKeys starts a server with user keys
func testStartServerWithKeys(t *testing.T) *Server {
	t.Helper()
	server := testStartServer(t)
	tmpDir := t.TempDir()
	if err := os.Setenv("SSH_HOME", tmpDir); err != nil {
		t.Fatalf("failed to set SSH_HOME: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("SSH_HOME")
	})

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}
	if err := testWriteKeypairToDir(pub, priv, tmpDir); err != nil {
		t.Fatalf("write keypair: %v", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("failed to create SSH public key: %v", err)
	}
	server.UserKeys = append(server.UserKeys, sshPub)
	return server
}

type Server struct {
	Addr     string          // host:port to connect to
	User     string          // default "alice"
	Password string          // default "secret"
	Key      ssh.Signer      // server host key
	UserKeys []ssh.PublicKey // authorized user keys, can be added later as well
	RootDir  string          // temp dir used as chroot-like base for SFTP & exec
	srv      *gliderssh.Server
	lis      net.Listener
}

func (s *Server) Hostname() string {
	host, _, _ := net.SplitHostPort(s.Addr)
	return host
}

func (s *Server) Port() int {
	_, port, _ := net.SplitHostPort(s.Addr)
	p, _ := strconv.Atoi(port)
	return p
}

func (s *Server) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = s.srv.Shutdown(ctx)
	_ = s.lis.Close()
}

// ClientConfig returns a client config matching the test server creds.
func (s *Server) ClientConfig() *ssh.ClientConfig {
	return &ssh.ClientConfig{
		User:            s.User,
		Auth:            []ssh.AuthMethod{ssh.Password(s.Password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // for tests
		Timeout:         3 * time.Second,
	}
}

// minimal flag parsing + both directions (-f and -t)
type scpOpts struct {
	from      bool // -f (client pulls from server)
	to        bool // -t (client pushes to server)
	recursive bool // -r (ignored in this minimal impl)
	preserve  bool // -p (ignored here; timestamps line `T` is parsed/ignored)
	target    string
}

// parse "scp [flags] <target>"
func parseScpArgs(args []string) (scpOpts, error) {
	var o scpOpts
	// skip "scp"
	for i := 1; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			if i+1 < len(args) {
				o.target = args[i+1]
			}
			break
		}
		if strings.HasPrefix(a, "-") {
			if strings.Contains(a, "f") {
				o.from = true
			}
			if strings.Contains(a, "t") {
				o.to = true
			}
			if strings.Contains(a, "r") {
				o.recursive = true
			}
			if strings.Contains(a, "p") {
				o.preserve = true
			}
			continue
		}
		o.target = a
	}
	if !o.from && !o.to {
		return o, fmt.Errorf("unsupported scp mode: need -f or -t")
	}
	if o.target == "" {
		return o, fmt.Errorf("missing target path")
	}
	return o, nil
}

// ---------- receive path: implement `scp -t <target>` (upload) ----------

func scpReceive(sess gliderssh.Session, root, target string) error {
	// RFC-ish handshake helpers
	readByte := func() (byte, error) {
		var b [1]byte
		_, err := io.ReadFull(sess, b[:])
		return b[0], err
	}
	sendAck := func() error { _, err := sess.Write([]byte{0}); return err }
	sendErr := func(msg string) error {
		_, _ = fmt.Fprintf(sess.Stderr(), "scp: %s\n", msg)
		_, err := sess.Write([]byte{2}) // fatal
		return err
	}

	// Tell the client we're ready
	if err := sendAck(); err != nil {
		return err
	}

	// We implement a tiny subset:
	//   optional: T <mtime> 0 <atime> 0\n   (ignore but ack)
	//   required: C<mode> <size> <name>\n
	//   then <size> bytes of data
	//   then a single 0 byte from client (end-of-file)
	//   we ack (0)
	//   end (client closes or sends next file; we handle one file)
	r := bufio.NewReader(sess)

	// If client sends T line, consume & ack (ignored)
	// Peek first byte; if 'T' consume the line, ack, continue
	if b, _ := r.Peek(1); len(b) == 1 && b[0] == 'T' {
		if _, err := r.ReadString('\n'); err != nil {
			return err
		}
		if err := sendAck(); err != nil {
			return err
		}
	}

	// Expect C line
	hdr, err := r.ReadString('\n')
	if err != nil {
		return err
	}
	if !strings.HasPrefix(hdr, "C") {
		_ = sendErr("only single-file uploads (C...) supported")
		return fmt.Errorf("unexpected header: %q", hdr)
	}

	// Parse: C%04o <size> <name>\n
	var modeStr string
	var size int64
	var name string
	_, err = fmt.Sscanf(hdr, "C%s %d %s\n", &modeStr, &size, &name)
	if err != nil {
		_ = sendErr("bad C header")
		return fmt.Errorf("parse header: %w", err)
	}
	// Ack header
	if err := sendAck(); err != nil {
		return err
	}

	// Open destination file under root/target
	dstPath := filepath.Clean(filepath.Join(root, target))
	// If target is a directory, place into it using the sent name
	if fi, statErr := os.Stat(dstPath); statErr == nil && fi.IsDir() {
		dstPath = filepath.Join(dstPath, name)
	}
	// Parse mode
	m, _ := strconv.ParseUint(modeStr, 8, 32)
	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(m)&0777)
	if err != nil {
		_ = sendErr("cannot open dest")
		return err
	}
	defer func() { _ = dst.Close() }()

	// Copy exactly <size> bytes
	if _, err := io.CopyN(dst, r, size); err != nil {
		_ = sendErr("short write")
		return err
	}

	// Consume file-end byte from client
	b, err := readByte()
	if err != nil && !errors.Is(err, io.EOF) {
		_ = sendErr("missing file-end ack")
		return fmt.Errorf("expected end-of-file 0, got %v err=%v", b, err)
	}
	// Final ack
	if err := sendAck(); err != nil {
		return err
	}

	return nil
}

// ---------- your exec handler using the parser + both paths ----------

func scpExecHandler(root string) gliderssh.Handler {
	return func(sess gliderssh.Session) {
		args := sess.Command()
		if len(args) == 0 || args[0] != "scp" {
			_, _ = io.WriteString(sess.Stderr(), "unsupported command\n")
			_ = sess.Exit(127)
			return
		}

		opts, err := parseScpArgs(args)
		if err != nil {
			_, _ = io.WriteString(sess.Stderr(), "scp: "+err.Error()+"\n")
			_ = sess.Exit(2)
			return
		}

		if opts.from {
			// existing send code (download): scp -f <target>
			fp := filepath.Clean(filepath.Join(root, opts.target))
			f, err := os.Open(fp)
			if err != nil {
				_ = sess.Exit(1)
				return
			}
			defer func() { _ = f.Close() }()
			fi, _ := f.Stat()
			// 1) wait for initial OK
			var b [1]byte
			if _, err := io.ReadFull(sess, b[:]); err != nil || b[0] != 0 {
				_ = sess.Exit(1)
				return
			}
			// 2) header
			_, _ = fmt.Fprintf(sess, "C%04o %d %s\n", fi.Mode()&0777, fi.Size(), filepath.Base(fp))
			// 3) wait OK
			if _, err := io.ReadFull(sess, b[:]); err != nil || b[0] != 0 {
				_ = sess.Exit(1)
				return
			}
			// 4) data
			if _, err := io.Copy(sess, f); err != nil {
				_ = sess.Exit(1)
				return
			}
			// 5) end + wait final OK
			_, _ = sess.Write([]byte{0})
			_, _ = io.ReadFull(sess, b[:])
			_ = sess.Exit(0)
			return
		}

		if opts.to {
			// NEW: receive upload: scp -t <target>
			if opts.recursive {
				_, _ = io.WriteString(sess.Stderr(), "scp: -r not supported\n")
				_ = sess.Exit(2)
				return
			}
			if err := scpReceive(sess, root, opts.target); err != nil {
				_ = sess.Exit(1)
				return
			}
			_ = sess.Exit(0)
			return
		}

		_, _ = io.WriteString(sess.Stderr(), "unsupported scp mode\n")
		_ = sess.Exit(2)
	}
}
