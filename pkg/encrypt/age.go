package encrypt

import (
	"bytes"
	"fmt"
	"io"

	"filippo.io/age"
)

var _ Encryptor = &AgeChacha20Poly1305{}

type AgeChacha20Poly1305 struct {
	recipientPubKey string
	identityKey     string
}

func NewAgeChacha20Poly1305(recipientPubKey []byte) (*AgeChacha20Poly1305, error) {
	key := string(recipientPubKey)
	return &AgeChacha20Poly1305{recipientPubKey: key}, nil
}

func (a *AgeChacha20Poly1305) Name() string {
	return string(AlgoAgeChacha20Poly1305)
}

func (a *AgeChacha20Poly1305) Description() string {
	return "age format with encryption using chacha20 and poly1305."
}

func (a *AgeChacha20Poly1305) Decrypt(out io.Writer) (io.WriteCloser, error) {
	identity, err := age.ParseX25519Identity(a.identityKey)
	if err != nil {
		return nil, fmt.Errorf("invalid age identity: %w", err)
	}

	// Buffer encrypted input until Close()
	buf := &bytes.Buffer{}
	return &ageDecryptWriter{
		identity: identity,
		buf:      buf,
		out:      out,
	}, nil
}

func (a *AgeChacha20Poly1305) Encrypt(out io.Writer) (io.WriteCloser, error) {
	// Parse the recipient's X25519 public key
	recipient, err := age.ParseX25519Recipient(a.recipientPubKey)
	if err != nil {
		return nil, fmt.Errorf("invalid recipient public key: %w", err)
	}

	// Create an age Writer that encrypts to the recipient
	ageWriter, err := age.Encrypt(out, recipient)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize age writer: %w", err)
	}

	return ageWriter, nil // already an io.WriteCloser
}

type ageDecryptWriter struct {
	identity *age.X25519Identity
	buf      *bytes.Buffer
	out      io.Writer
	closed   bool
}

func (w *ageDecryptWriter) Write(p []byte) (int, error) {
	return w.buf.Write(p)
}

func (w *ageDecryptWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true

	reader, err := age.Decrypt(w.buf, w.identity)
	if err != nil {
		return fmt.Errorf("age decryption failed: %w", err)
	}

	_, err = io.Copy(w.out, reader)
	return err
}
