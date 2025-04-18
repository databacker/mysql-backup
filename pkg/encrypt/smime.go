package encrypt

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"

	cms "github.com/InfiniteLoopSpace/go_S-MIME/cms"
)

var _ Encryptor = &SMimeAES256CBC{}

type SMimeAES256CBC struct {
	recipientCert *x509.Certificate
}

func NewSMimeAES256CBC(certPEM []byte) (*SMimeAES256CBC, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode recipient cert PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("invalid recipient cert: %w", err)
	}
	return &SMimeAES256CBC{recipientCert: cert}, nil
}

func (s *SMimeAES256CBC) Name() string {
	return string(AlgoSMimeAES256CBC)
}

func (s *SMimeAES256CBC) Description() string {
	return "SMIME with AES256-CBC encryption. Should work with `openssl smime -decrypt -inform DER -recip <cert.pem> -inkey <key.pem>`"
}

func (s *SMimeAES256CBC) Decrypt(out io.Writer) (io.WriteCloser, error) {
	return nil, fmt.Errorf("decrypt not implemented for SMIME")
}

func (s *SMimeAES256CBC) Encrypt(out io.Writer) (io.WriteCloser, error) {
	buf := &bytes.Buffer{}
	return &streamingEncryptWriter{
		recipient: s.recipientCert,
		plaintext: buf,
		out:       out,
	}, nil
}

type streamingEncryptWriter struct {
	recipient *x509.Certificate
	plaintext *bytes.Buffer
	out       io.Writer
	closed    bool
}

func (w *streamingEncryptWriter) Write(p []byte) (int, error) {
	return w.plaintext.Write(p)
}

func (w *streamingEncryptWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true

	// Create a new S/MIME wrapper
	s, err := cms.New()
	if err != nil {
		return fmt.Errorf("failed to create smime instance: %w", err)
	}

	// Encrypt the buffered plaintext using AES-256-CBC and wrap in CMS
	der, err := s.Encrypt(w.plaintext.Bytes(), []*x509.Certificate{w.recipient})
	if err != nil {
		return fmt.Errorf("S/MIME encryption failed: %w", err)
	}

	_, err = w.out.Write(der)
	return err
}
