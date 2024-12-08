package config

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	utiltest "github.com/databacker/mysql-backup/pkg/internal/test"
	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/nacl/box"
	"gopkg.in/yaml.v3"

	"github.com/databacker/api/go/api"
	"github.com/google/go-cmp/cmp"
)

func TestGetRemoteConfig(t *testing.T) {
	configFile := "./testdata/config.yml"
	content, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}
	var validConfig api.Config
	if err := yaml.Unmarshal(content, &validConfig); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}
	// start the server before the tests
	server, fingerprint, clientKeys, err := utiltest.StartServer(1, func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		f, err := os.Open(configFile)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(err.Error()))
			return
		}
		if _, err = buf.ReadFrom(f); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(err.Error()))
			return
		}
		if _, err := w.Write(buf.Bytes()); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(err.Error()))
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Close()
	tests := []struct {
		name   string
		url    string
		err    string
		config api.Config
	}{
		{"no url", "", "unsupported protocol scheme", api.Config{}},
		{"invalid server", "https://foo.bar/com", "no such host", api.Config{}},
		{"no path", "https://google.com/foo/bar/abc", "invalid config file", api.Config{}},
		{"nothing listening", "https://localhost:12345/foo/bar/abc", "connection refused", api.Config{}},
		{"valid", server.URL, "", validConfig},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creds := base64.StdEncoding.EncodeToString(clientKeys[0])
			spec := api.RemoteSpec{
				URL:          &tt.url,
				Certificates: &[]string{fingerprint},
				Credentials:  &creds,
			}
			conf, err := getRemoteConfig(spec)
			switch {
			case tt.err == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tt.err != "" && err == nil:
				t.Fatalf("expected error: %s", tt.err)
			case tt.err != "" && !strings.Contains(err.Error(), tt.err):
				t.Fatalf("mismatched error: %s, got: %v", tt.err, err)
			default:
				diff := cmp.Diff(tt.config, conf)
				if diff != "" {
					t.Fatalf("mismatched config: %s", diff)
				}
			}
		})
	}

}

func TestDecryptConfig(t *testing.T) {
	configFile := "./testdata/config.yml"
	content, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}
	var validConfig api.Config
	if err := yaml.Unmarshal(content, &validConfig); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}

	senderCurve := ecdh.X25519()
	senderPrivateKey, err := senderCurve.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("failed to generate sender random seed: %v", err)
	}
	senderPublicKey := senderPrivateKey.PublicKey()
	senderPublicKeyBytes := senderPublicKey.Bytes()

	recipientCurve := ecdh.X25519()
	recipientPrivateKey, err := recipientCurve.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("failed to generate recipient random seed: %v", err)
	}
	recipientPublicKey := recipientPrivateKey.PublicKey()
	recipientPublicKeyBytes := recipientPublicKey.Bytes()

	var recipientPublicKeyArray, senderPrivateKeyArray [32]byte
	copy(recipientPublicKeyArray[:], recipientPublicKeyBytes)
	copy(senderPrivateKeyArray[:], senderPrivateKey.Bytes())

	senderPublicKeyB64 := base64.StdEncoding.EncodeToString(senderPublicKeyBytes)

	recipientPublicKeyB64 := base64.StdEncoding.EncodeToString(recipientPublicKeyBytes)

	// compute the shared secret using the sender's private key and the recipient's public key
	var sharedSecret [32]byte
	box.Precompute(&sharedSecret, &recipientPublicKeyArray, &senderPrivateKeyArray)

	// Derive the symmetric key using HKDF with the shared secret
	hkdfReader := hkdf.New(sha256.New, sharedSecret[:], nil, []byte(api.SymmetricKey))
	symmetricKey := make([]byte, 32) // AES-GCM requires 32 bytes
	if _, err := hkdfReader.Read(symmetricKey); err != nil {
		t.Fatalf("failed to derive symmetric key: %v", err)
	}

	// Create AES cipher block
	block, err := aes.NewCipher(symmetricKey)
	if err != nil {
		t.Fatalf("failed to create AES cipher")
	}
	// Create GCM instance
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("failed to create AES-GCM")
	}

	// Generate a random nonce
	nonce := make([]byte, aesGCM.NonceSize())
	_, err = cryptorand.Read(nonce)
	if err != nil {
		t.Fatalf("failed to generate nonce")
	}

	// Encrypt the plaintext
	ciphertext := aesGCM.Seal(nil, nonce, content, nil)

	// Embed the nonce in the ciphertext
	fullCiphertext := append(nonce, ciphertext...)

	algo := api.AesGcm256
	data := base64.StdEncoding.EncodeToString(fullCiphertext)

	// this is a valid spec, we want to be able to change fields
	// without modifying the original, so we have a utility function after
	validSpec := api.EncryptedSpec{
		Algorithm:          &algo,
		Data:               &data,
		RecipientPublicKey: &recipientPublicKeyB64,
		SenderPublicKey:    &senderPublicKeyB64,
	}

	// copy a spec, changing specific fields
	copyModifySpec := func(opts ...func(*api.EncryptedSpec)) api.EncryptedSpec {
		copy := validSpec
		for _, opt := range opts {
			opt(&copy)
		}
		return copy
	}

	unusedSeed := make([]byte, ed25519.SeedSize)
	if _, err := io.ReadFull(cryptorand.Reader, unusedSeed); err != nil {
		t.Fatalf("failed to generate sender random seed: %v", err)
	}

	// recipient private key credentials
	recipientCreds := []string{base64.StdEncoding.EncodeToString(recipientPrivateKey.Bytes())}
	unusedCreds := []string{base64.StdEncoding.EncodeToString(unusedSeed)}

	tests := []struct {
		name        string
		inSpec      api.EncryptedSpec
		credentials []string
		config      api.Config
		err         error
	}{
		{"no algorithm", copyModifySpec(func(s *api.EncryptedSpec) { s.Algorithm = nil }), recipientCreds, api.Config{}, errors.New("empty algorithm")},
		{"no data", copyModifySpec(func(s *api.EncryptedSpec) { s.Data = nil }), recipientCreds, api.Config{}, errors.New("empty data")},
		{"bad base64 data", copyModifySpec(func(s *api.EncryptedSpec) { data := "abcdef"; s.Data = &data }), recipientCreds, api.Config{}, errors.New("failed to decode encrypted data: illegal base64 data")},
		{"short encrypted data", copyModifySpec(func(s *api.EncryptedSpec) {
			data := base64.StdEncoding.EncodeToString([]byte("abcdef"))
			s.Data = &data
		}), recipientCreds, api.Config{}, errors.New("invalid encrypted data length")},
		{"invalid encrypted data", copyModifySpec(func(s *api.EncryptedSpec) {
			bad := nonce
			bad = append(bad, 1, 2, 3, 4)
			data := base64.StdEncoding.EncodeToString(bad)
			s.Data = &data
		}), recipientCreds, api.Config{}, errors.New("failed to decrypt data: cipher: message authentication failed")},
		{"empty credentials", validSpec, nil, api.Config{}, errors.New("no private key found that matches public key")},
		{"unmatched credentials", validSpec, unusedCreds, api.Config{}, errors.New("no private key found that matches public key")},
		{"success with just one credential", validSpec, recipientCreds, validConfig, nil},
		{"success with multiple credentials", validSpec, append(recipientCreds, unusedCreds...), validConfig, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf, err := decryptConfig(tt.inSpec, tt.credentials)
			switch {
			case err == nil && tt.err != nil:
				t.Fatalf("expected error: %v", tt.err)
			case err != nil && tt.err == nil:
				t.Fatalf("unexpected error: %v", err)
			case err != nil && tt.err != nil && !strings.HasPrefix(err.Error(), tt.err.Error()):
				t.Fatalf("mismatched error: %v", err)
			}
			diff := cmp.Diff(tt.config, conf)
			if diff != "" {
				t.Fatalf("mismatched config: %s", diff)
			}
		})
	}
}
