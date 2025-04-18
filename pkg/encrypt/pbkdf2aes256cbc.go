package encrypt

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"

	"golang.org/x/crypto/pbkdf2"
)

const (
	pbkdf2KeyLen     = 32
	pbkdf2SaltSize   = 16
	pbkdf2Iterations = 10000
)

var _ Encryptor = &PBKDF2AES256CBC{}

type PBKDF2AES256CBC struct {
	passphrase []byte
}

func NewPBKDF2AES256CBC(passphrase []byte) (*PBKDF2AES256CBC, error) {
	if len(passphrase) == 0 {
		return nil, fmt.Errorf("passphrase cannot be empty")
	}
	return &PBKDF2AES256CBC{passphrase: passphrase}, nil
}

func (s *PBKDF2AES256CBC) Name() string {
	return string(AlgoPBKDF2AES256CBC)
}

func (s *PBKDF2AES256CBC) Description() string {
	return "PBKDF2 with AES256-CBC encryption. Should work with `openssl enc -d -aes-256-cbc -pbkdf2 -pass <pass-encoding>`"
}

func (s *PBKDF2AES256CBC) Decrypt(out io.Writer) (io.WriteCloser, error) {
	// Return a WriteCloser that buffers the salt, derives the key, and then streams decryption
	pr := &pbkdf2DecryptReader{
		passphrase: s.passphrase,
		out:        out,
		buf:        make([]byte, 0, pbkdf2SaltSize),
	}
	return pr, nil
}

func (s *PBKDF2AES256CBC) Encrypt(out io.Writer) (io.WriteCloser, error) {
	// Step 1: Generate a random salt (used by OpenSSL)
	salt := make([]byte, 8)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}

	// Step 3: Derive 32-byte key using PBKDF2 with SHA-256
	keyComplete := pbkdf2.Key(s.passphrase, salt, pbkdf2Iterations, 48, sha256.New)
	key := keyComplete[:pbkdf2KeyLen]
	iv := keyComplete[32:]

	// Step 3: Write non-standard header and salt to output stream
	if _, err := out.Write([]byte("Salted__")); err != nil {
		return nil, fmt.Errorf("failed to write salt: %w", err)
	}
	if _, err := out.Write(salt); err != nil {
		return nil, fmt.Errorf("failed to write salt: %w", err)
	}

	// Step 4: Delegate to AES256CBC using the derived key
	aes, err := NewAES256CBC(key, iv, false)
	if err != nil {
		return nil, err
	}
	return aes.Encrypt(out)
}

type pbkdf2DecryptReader struct {
	passphrase []byte
	out        io.Writer
	buf        []byte
	aes        io.WriteCloser
	err        error
	closed     bool
}

func (r *pbkdf2DecryptReader) Write(p []byte) (int, error) {
	if r.aes != nil {
		return r.aes.Write(p)
	}

	// Buffer salt
	needed := pbkdf2SaltSize - len(r.buf)
	if needed > len(p) {
		r.buf = append(r.buf, p...)
		return len(p), nil
	}

	r.buf = append(r.buf, p[:needed]...)
	// Derive key
	key := pbkdf2.Key(r.passphrase, r.buf, pbkdf2Iterations, 32, sha256.New)

	// Initialize AES decryption
	aes, err := NewAES256CBC(key, nil, false)
	if err != nil {
		r.err = err
		return 0, err
	}

	r.aes, err = aes.Decrypt(r.out)
	if err != nil {
		r.err = err
		return 0, err
	}

	// Write remaining data after salt
	n, err := r.aes.Write(p[needed:])
	return needed + n, err
}

func (r *pbkdf2DecryptReader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	if r.aes != nil {
		return r.aes.Close()
	}
	return r.err
}
