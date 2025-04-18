package encrypt

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
)

var _ Encryptor = &AES256CBC{}

type AES256CBC struct {
	key       []byte
	iv        []byte
	prependIV bool
}

func NewAES256CBC(key, iv []byte, prependIV bool) (*AES256CBC, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("key must be 32 bytes")
	}
	return &AES256CBC{
		key:       key,
		iv:        iv,
		prependIV: prependIV,
	}, nil
}

type cbcEncryptWriter struct {
	writer io.Writer
	mode   cipher.BlockMode
	buf    []byte
	closed bool
}

func (w *cbcEncryptWriter) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		n := aes.BlockSize - len(w.buf)
		if n > len(p) {
			n = len(p)
		}
		w.buf = append(w.buf, p[:n]...)
		p = p[n:]
		total += n

		if len(w.buf) == aes.BlockSize {
			block := make([]byte, aes.BlockSize)
			w.mode.CryptBlocks(block, w.buf)
			if _, err := w.writer.Write(block); err != nil {
				return total, err
			}
			w.buf = w.buf[:0]
		}
	}
	return total, nil
}

func (w *cbcEncryptWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true

	// PKCS#7 padding
	padLen := aes.BlockSize - len(w.buf)%aes.BlockSize
	padding := bytes.Repeat([]byte{byte(padLen)}, padLen)
	w.buf = append(w.buf, padding...)

	// Encrypt all remaining blocks in w.buf
	out := make([]byte, len(w.buf))
	w.mode.CryptBlocks(out, w.buf)

	// Write to the underlying writer
	_, err := w.writer.Write(out)
	return err
}

type cbcDecryptWriter struct {
	writer io.Writer
	block  cipher.Block
	mode   cipher.BlockMode
	buf    []byte
	iv     []byte
	closed bool
	readIV bool
}

func (w *cbcDecryptWriter) Write(p []byte) (int, error) {
	total := 0
	w.buf = append(w.buf, p...)

	if !w.readIV && len(w.buf) >= aes.BlockSize {
		w.iv = w.buf[:aes.BlockSize]
		w.mode = cipher.NewCBCDecrypter(w.block, w.iv)
		w.buf = w.buf[aes.BlockSize:]
		w.readIV = true
	}

	for len(w.buf) >= aes.BlockSize*2 {
		block := w.buf[:aes.BlockSize]
		dst := make([]byte, aes.BlockSize)
		w.mode.CryptBlocks(dst, block)
		if _, err := w.writer.Write(dst); err != nil {
			return total, err
		}
		w.buf = w.buf[aes.BlockSize:]
		total += aes.BlockSize
	}

	return total, nil
}

func (w *cbcDecryptWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true

	if len(w.buf) != aes.BlockSize {
		return fmt.Errorf("incomplete final block")
	}
	block := make([]byte, aes.BlockSize)
	w.mode.CryptBlocks(block, w.buf)

	// Remove PKCS#7 padding
	padLen := int(block[len(block)-1])
	if padLen <= 0 || padLen > aes.BlockSize {
		return fmt.Errorf("invalid padding")
	}
	for _, b := range block[len(block)-padLen:] {
		if int(b) != padLen {
			return fmt.Errorf("invalid padding content")
		}
	}
	_, err := w.writer.Write(block[:len(block)-padLen])
	return err
}
func (s *AES256CBC) Name() string {
	return string(AlgoDirectAES256CBC)
}

func (s *AES256CBC) Description() string {
	return "AES-256-CBC output format with IV prepended; should work with `openssl enc -d -aes-256-cbc -K <key-in-hex> -iv auto`."
}

func (s *AES256CBC) Decrypt(out io.Writer) (io.WriteCloser, error) {
	if len(s.key) != 32 {
		return nil, fmt.Errorf("key must be 32 bytes")
	}

	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}

	// The returned WriteCloser will buffer input until it receives at least one full block
	return &cbcDecryptWriter{
		writer: out,
		block:  block,
	}, nil
}

func (s *AES256CBC) Encrypt(out io.Writer) (io.WriteCloser, error) {
	if len(s.key) != 32 {
		return nil, fmt.Errorf("key must be 32 bytes")
	}

	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}

	iv := s.iv
	if len(iv) == 0 {
		iv = make([]byte, aes.BlockSize)
		if _, err := rand.Read(iv); err != nil {
			return nil, fmt.Errorf("failed to generate IV: %w", err)
		}
	}
	if s.prependIV {
		if _, err := out.Write(iv); err != nil {
			return nil, fmt.Errorf("failed to write IV to output: %w", err)
		}
	}

	mode := cipher.NewCBCEncrypter(block, iv)

	return &cbcEncryptWriter{
		writer: out,
		mode:   mode,
		buf:    make([]byte, 0, aes.BlockSize),
	}, nil
}
