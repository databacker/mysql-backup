package scp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"time"

	scp "github.com/bramvdbogaerde/go-scp"
	"github.com/kevinburke/ssh_config"
	"github.com/pkg/sftp"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

var baseIdentityFileNames = []string{
	"id_ed25519",
	"id_ecdsa",
	"id_ecdsa_sk",   // FIDO2
	"id_ed25519_sk", // FIDO2
	"id_rsa",        // still common, though SHA-1 is discouraged
}

func getIdentityFiles() []string {
	idFileDir := os.Getenv("SSH_HOME")
	if idFileDir == "" {
		idFileDir = filepath.Join(os.Getenv("HOME"), ".ssh")
	}
	var files []string
	for _, name := range baseIdentityFileNames {
		filename := filepath.Join(idFileDir, name)
		stat, err := os.Stat(filename) // check if file exists
		if err == nil && !stat.IsDir() {
			files = append(files, filename)
		}
	}
	return files
}

type SCP struct {
	url url.URL
}

func New(u url.URL) *SCP {
	return &SCP{u}
}

func (s *SCP) Pull(ctx context.Context, source, target string, logger *log.Entry) (int64, error) {
	client, err := s.getSCPClient()
	if err != nil {
		return 0, fmt.Errorf("failed to create SCP client: %w", err)
	}
	f, err := os.OpenFile(target, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return 0, fmt.Errorf("failed to open target file %s: %w", target, err)
	}

	defer func() {
		// Close client connection after the file has been copied
		client.Close()
		// Close the file after it has been copied
		_ = f.Close()
	}()

	if err := client.CopyFromRemote(ctx, f, source); err != nil {
		return 0, fmt.Errorf("failed to copy file from SCP server: %w", err)
	}
	stat, err := f.Stat()
	if err != nil {
		return 0, fmt.Errorf("failed to get file stat: %w", err)
	}
	return stat.Size(), nil
}

func (s *SCP) Push(ctx context.Context, target, source string, logger *log.Entry) (int64, error) {
	client, err := s.getSCPClient()
	if err != nil {
		return 0, fmt.Errorf("failed to create SCP client: %w", err)
	}

	f, err := os.Open(source)
	if err != nil {
		return 0, fmt.Errorf("failed to open source file %s: %w", source, err)
	}

	defer func() {
		// Close client connection after the file has been copied
		client.Close()
		// Close the file after it has been copied
		_ = f.Close()
	}()

	if err := client.CopyFromFile(ctx, *f, target, "0644"); err != nil {
		return 0, fmt.Errorf("failed to copy file to SCP server: %w", err)
	}
	stat, err := f.Stat()
	if err != nil {
		return 0, fmt.Errorf("failed to get file stat: %w", err)
	}
	return stat.Size(), nil
}

func (s *SCP) Clean(filename string) string {
	return filename
}

func (s *SCP) Protocol() string {
	return "scp"
}

func (s *SCP) URL() string {
	return s.url.String()
}

func (s *SCP) ReadDir(ctx context.Context, dirname string, logger *log.Entry) ([]fs.FileInfo, error) {
	client, err := s.getSSHClient()
	if err != nil {
		return nil, err
	}
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sftpClient.Close() }()
	infos, err := sftpClient.ReadDir(dirname)
	if err != nil {
		return nil, fmt.Errorf("failed to read remote directory %s over sftp: %w", dirname, err)
	}
	out := make([]fs.FileInfo, 0, len(infos))
	out = append(out, infos...)
	return out, nil
}

func (s *SCP) Remove(ctx context.Context, target string, logger *log.Entry) error {
	client, err := s.getSSHClient()
	if err != nil {
		return err
	}
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return fmt.Errorf("new sftp: %w", err)
	}
	defer func() { _ = sftpClient.Close() }()

	if err := sftpClient.Remove(target); err != nil {
		return fmt.Errorf("remove %q: %w", target, err)
	}
	return nil
}

// command run a command over ssh
//
//nolint:unparam,unused
func (s *SCP) command(ctx context.Context, cmd string) (stdout string, stderr string, err error) {
	client, err := s.getSSHClient()
	if err != nil {
		return "", "", fmt.Errorf("failed to create SSH client: %w", err)
	}
	// Step 1: Create session
	session, err := client.NewSession()
	if err != nil {
		return "", "", err
	}
	defer func() { _ = session.Close() }()

	// Step 2: Capture stdout and stderr
	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	// Step 3: Run command
	err = session.Run(cmd) // blocks until command finishes

	// Step 4: Return captured outputs
	return stdoutBuf.String(), stderrBuf.String(), err

}

func (s *SCP) getSSHClient() (*ssh.Client, error) {
	// read ssh config file
	sshConfig, err := loadSSHConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load SSH config: %w", err)
	}
	// check items as provided against ssh config, URL override, defaults
	hostname := s.url.Hostname()
	port := s.url.Port()
	username := s.url.User.Username()
	var identityFiles []string
	configPort, err := sshConfig.Get(hostname, "Port")
	if err != nil {
		return nil, fmt.Errorf("error getting hostname from SSH config: %w", err)
	}
	configHostname, err := sshConfig.Get(hostname, "HostName")
	if err != nil {
		return nil, fmt.Errorf("error getting port from SSH config: %w", err)
	}
	configIdentityFile, err := sshConfig.Get(hostname, "IdentityFile")
	if err != nil {
		return nil, fmt.Errorf("error getting identity file from SSH config: %w", err)
	}
	configUsername, err := sshConfig.Get(hostname, "User")
	if err != nil {
		return nil, fmt.Errorf("error getting username from SSH config: %w", err)
	}
	if configPort != "" {
		port = configPort
	}
	if configHostname != "" {
		hostname = configHostname
	}
	if configUsername != "" {
		username = configUsername
	}
	if configIdentityFile != "" {
		identityFiles = append(identityFiles, configIdentityFile)
	}
	// look for fixed identity files, if none explicitly specified
	if len(identityFiles) == 0 {
		identityFiles = getIdentityFiles()
	}
	authMethods, err := authMethodsFromAgentAndFiles(identityFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to get SSH auth methods: %w", err)
	}
	clientConfig := &ssh.ClientConfig{
		User:            username,
		Auth:            authMethods,
		Timeout:         15 * time.Second,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO: use a proper host key callback in production
	}

	client, err := ssh.Dial("tcp", net.JoinHostPort(hostname, port), clientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SSH server: %w", err)
	}
	return client, nil
}

func (s *SCP) getSCPClient() (*scp.Client, error) {
	sshClient, err := s.getSSHClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create SSH client: %w", err)
	}
	// Create a new SCP client
	client, err := scp.NewClientBySSH(sshClient)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SCP server: %w", err)
	}
	return &client, nil
}

func loadSSHConfig() (*ssh_config.Config, error) {
	idFileDir := os.Getenv("SSH_HOME")
	if idFileDir == "" {
		idFileDir = filepath.Join(os.Getenv("HOME"), ".ssh")
	}
	path := filepath.Join(idFileDir, "config")
	f, err := os.Open(path)
	if err != nil {
		// No config is fine; act like empty config.
		return &ssh_config.Config{}, nil
	}
	defer func() { _ = f.Close() }()
	return ssh_config.Decode(f)
}

func authMethodsFromAgentAndFiles(identityFiles []string) ([]ssh.AuthMethod, error) {
	var (
		methods []ssh.AuthMethod
		signers []ssh.Signer
	)
	// ssh-agent
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if conn, err := net.Dial("unix", sock); err == nil {
			agentSigners, err := agent.NewClient(conn).Signers()
			if err != nil {
				return nil, fmt.Errorf("failed to get signers from SSH agent: %v", err)
			}
			signers = append(signers, agentSigners...)
		}
	}

	// Identity files
	for _, p := range identityFiles {
		key, err := os.ReadFile(p)
		// skip missing files gracefully
		if err != nil {
			continue
		}
		raw, err := ssh.ParseRawPrivateKey(key)
		if err != nil && errors.Is(err, &ssh.PassphraseMissingError{}) {
			// ignore encrypted keys
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key %s: %w", p, err)
		}
		signer, err := ssh.NewSignerFromKey(raw)
		if err != nil {
			return nil, fmt.Errorf("failed to create signer from key %s: %w", p, err)
		}
		signers = append(signers, signer)
	}
	methods = append(methods, ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
		// assemble signers here (file first, then agent)
		return signers, nil
	}))

	return methods, nil
}
