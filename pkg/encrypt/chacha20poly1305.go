package encrypt

import (
	"bytes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
)

var _ Encryptor = &Chacha20Poly1305{}

type Chacha20Poly1305 struct {
	key []byte
}

func NewChacha20Poly1305(key []byte) (*Chacha20Poly1305, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("key length must be 32 bytes for Chacha20Poly1305, not %d", len(key))
	}
	return &Chacha20Poly1305{key: key}, nil
}

func (s *Chacha20Poly1305) Name() string {
	return string(AlgoChacha20Poly1305)
}

func (s *Chacha20Poly1305) Description() string {
	return "Chacha20-Poly1305 encryption."
}

func (s *Chacha20Poly1305) Decrypt(out io.Writer) (io.WriteCloser, error) {
	if len(s.key) != chacha20poly1305.KeySize {
		return nil, fmt.Errorf("key must be 32 bytes")
	}

	aead, err := chacha20poly1305.New(s.key)
	if err != nil {
		return nil, fmt.Errorf("failed to create chacha20poly1305: %w", err)
	}

	return &chacha20DecryptWriter{
		aead: aead,
		out:  out,
		buf:  &bytes.Buffer{},
	}, nil
}

func (s *Chacha20Poly1305) Encrypt(out io.Writer) (io.WriteCloser, error) {
	if len(s.key) != chacha20poly1305.KeySize {
		return nil, fmt.Errorf("key must be 32 bytes")
	}

	aead, err := chacha20poly1305.New(s.key)
	if err != nil {
		return nil, fmt.Errorf("failed to create chacha20poly1305: %w", err)
	}

	nonce := make([]byte, chacha20poly1305.NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Write the nonce first
	if _, err := out.Write(nonce); err != nil {
		return nil, fmt.Errorf("failed to write nonce: %w", err)
	}

	return &chacha20EncryptWriter{
		aead:   aead,
		nonce:  nonce,
		out:    out,
		buffer: &bytes.Buffer{},
	}, nil
}

type chacha20EncryptWriter struct {
	aead   cipher.AEAD
	nonce  []byte
	out    io.Writer
	buffer *bytes.Buffer
	closed bool
}

func (w *chacha20EncryptWriter) Write(p []byte) (int, error) {
	return w.buffer.Write(p)
}

func (w *chacha20EncryptWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true

	ciphertext := w.aead.Seal(nil, w.nonce, w.buffer.Bytes(), nil)
	_, err := w.out.Write(ciphertext)
	return err
}

type chacha20DecryptWriter struct {
	aead   cipher.AEAD
	out    io.Writer
	buf    *bytes.Buffer
	closed bool
}

func (w *chacha20DecryptWriter) Write(p []byte) (int, error) {
	return w.buf.Write(p)
}

func (w *chacha20DecryptWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true

	data := w.buf.Bytes()
	if len(data) < chacha20poly1305.NonceSize {
		return fmt.Errorf("missing nonce or ciphertext")
	}

	nonce := data[:chacha20poly1305.NonceSize]
	ciphertext := data[chacha20poly1305.NonceSize:]

	plaintext, err := w.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return fmt.Errorf("decryption failed: %w", err)
	}

	_, err = w.out.Write(plaintext)
	return err
}
