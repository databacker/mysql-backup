package encrypt

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/databacker/api/go/api"
	"golang.org/x/crypto/chacha20poly1305"
)

func TestEncryptors(t *testing.T) {
	cleartext, err := generateRandomCleartext(1024)
	if err != nil {
		t.Fatalf("failed to generate cleartext: %v", err)
	}

	for _, tt := range All {
		t.Run(tt, func(t *testing.T) {
			var (
				encKey, decKey []byte
				err            error
				tmpdir         = t.TempDir()
			)
			switch tt {
			case string(api.EncryptionAlgorithmSmimeAes256Cbc):
				encKey, decKey, err = generateSelfSignedCert()
				if err != nil {
					t.Fatalf("failed to generate self-signed cert: %v", err)
				}
				_ = os.WriteFile(filepath.Join(tmpdir, "cert.pem"), encKey, 0644)
				_ = os.WriteFile(filepath.Join(tmpdir, "key.pem"), decKey, 0644)
			case string(api.EncryptionAlgorithmAes256Cbc):
				encKey, err = generateRandomKey(32)
				if err != nil {
					t.Fatalf("failed to generate AES256 key: %v", err)
				}
				decKey = encKey
			case string(api.EncryptionAlgorithmPbkdf2Aes256Cbc):
				encKey = []byte("testpassword")
				decKey = encKey
			case string(api.EncryptionAlgorithmChacha20Poly1305):
				encKey, err = generateRandomKey(chacha20poly1305.KeySize)
				if err != nil {
					t.Fatalf("failed to generate AES256 key: %v", err)
				}
				decKey = encKey
			case string(api.EncryptionAlgorithmAgeChacha20Poly1305):
				// Step 2: Generate age key pair (X25519)
				identity, err := age.GenerateX25519Identity()
				if err != nil {
					t.Fatalf("failed to generate age identity: %v", err)
				}
				recipient := identity.Recipient().String() // string form of public key
				encKey = []byte(recipient)
				decKey = []byte(identity.String()) // string form of private key
			default:
				t.Fatalf("unsupported encryptor: %s", tt)
			}

			encryptor, err := GetEncryptor(tt, encKey)
			if err != nil {
				t.Fatalf("failed to get encryptor: %v", err)
			}
			var encrypted bytes.Buffer

			writer, err := encryptor.Encrypt(&encrypted)
			if err != nil {
				t.Fatalf("Encrypt setup failed: %v", err)
			}

			if _, err := writer.Write(cleartext); err != nil {
				t.Fatalf("writing to Encryptor failed: %v", err)
			}

			if err := writer.Close(); err != nil {
				t.Fatalf("closing Encryptor failed: %v", err)
			}

			switch encryptor.Name() {
			case string(api.EncryptionAlgorithmAes256Cbc):
				// Only works if IV is prepended
				b := encrypted.Bytes()
				if len(b) < 16 {
					t.Fatalf("ciphertext too short for AES256CBC: got %d bytes", len(b))
				}
				iv := b[:16]
				ciphertext := b[16:]
				keyHex := hex.EncodeToString(decKey)
				cmd := exec.Command("openssl", "enc",
					"-d", "-aes-256-cbc",
					"-K", keyHex,
					"-iv", hex.EncodeToString(iv)) // IV is embedded
				var (
					decrypted bytes.Buffer
					stderr    bytes.Buffer
				)
				cmd.Stdin = bytes.NewReader(ciphertext)
				cmd.Stdout = &decrypted
				cmd.Stderr = &stderr

				err := cmd.Run()
				if err != nil {
					t.Errorf("OpenSSL failed to decrypt AES256CBC: %v; %s", err, stderr.Bytes())
					return
				}

				if !bytes.Equal(decrypted.Bytes(), cleartext) {
					t.Error("decrypted output does not match original cleartext")
				}

			case string(api.EncryptionAlgorithmSmimeAes256Cbc):
				cmd := exec.Command("openssl", "smime",
					"-decrypt",
					"-inform", "DER",
					"-recip", filepath.Join(tmpdir, "cert.pem"),
					"-inkey", filepath.Join(tmpdir, "key.pem"))

				var (
					decrypted bytes.Buffer
					stderr    bytes.Buffer
				)
				cmd.Stdin = bytes.NewReader(encrypted.Bytes())
				cmd.Stdout = &decrypted
				cmd.Stderr = &stderr

				err := cmd.Run()
				if err != nil {
					t.Errorf("OpenSSL failed to decrypt SMIME: %v; %s", err, stderr.Bytes())
					return
				}

				if !bytes.Equal(decrypted.Bytes(), cleartext) {
					t.Error("SMIME decrypted output does not match original cleartext")
				}
			case string(api.EncryptionAlgorithmPbkdf2Aes256Cbc):
				cmd := exec.Command("openssl", "enc",
					"-d", "-aes-256-cbc",
					"-pbkdf2",
					"-pass", fmt.Sprintf("pass:%s", string(decKey)))

				var (
					decrypted bytes.Buffer
					stderr    bytes.Buffer
				)
				cmd.Stdin = bytes.NewReader(encrypted.Bytes())
				cmd.Stdout = &decrypted
				cmd.Stderr = &stderr

				if err := cmd.Run(); err != nil {
					t.Errorf("OpenSSL failed to decrypt PBKDF2AES256CBC: %v; %s", err, stderr.Bytes())
					return
				}

				if !bytes.Equal(decrypted.Bytes(), cleartext) {
					t.Error("PBKDF2AES256CBC decrypted output does not match original cleartext")
				}
			case string(api.EncryptionAlgorithmChacha20Poly1305):
				cipherData := encrypted.Bytes()

				const (
					nonceSize = 12
					tagSize   = 16
				)

				if len(cipherData) < nonceSize+tagSize {
					t.Fatalf("ciphertext too short for ChaCha20-Poly1305: got %d bytes", len(cipherData))
				}

				nonce := cipherData[:nonceSize]
				tag := cipherData[len(cipherData)-tagSize:]
				ciphertext := cipherData[nonceSize : len(cipherData)-tagSize]

				key := decKey
				if len(key) != chacha20poly1305.KeySize {
					t.Fatalf("invalid key size: expected %d bytes, got %d", chacha20poly1305.KeySize, len(key))
				}

				aead, err := chacha20poly1305.New(key)
				if err != nil {
					t.Fatalf("failed to create AEAD: %v", err)
				}

				// AEAD expects tag to be appended to ciphertext
				cipherAndTag := append(ciphertext, tag...)

				plaintext, err := aead.Open(nil, nonce, cipherAndTag, nil)
				if err != nil {
					t.Errorf("Go AEAD failed to decrypt ChaCha20Poly1305: %v", err)
					return
				}

				if !bytes.Equal(plaintext, cleartext) {
					t.Error("ChaCha20Poly1305 decrypted output does not match original cleartext")
				}
			case string(api.EncryptionAlgorithmAgeChacha20Poly1305):
				parsedIdentities, err := age.ParseIdentities(bytes.NewReader(decKey))
				if err != nil {
					t.Fatalf("failed to parse identity: %v", err)
				}

				if len(parsedIdentities) != 1 {
					t.Fatalf("expected 1 identity, got %d", len(parsedIdentities))
				}

				x25519Ident, ok := parsedIdentities[0].(*age.X25519Identity)
				if !ok {
					t.Fatalf("parsed identity is not X25519")
				}
				r := bytes.NewReader(encrypted.Bytes())
				ageReader, err := age.Decrypt(r, x25519Ident)
				if err != nil {
					t.Fatalf("failed to create age decryptor: %v", err)
				}

				decrypted, err := io.ReadAll(ageReader)
				if err != nil {
					t.Fatalf("failed to read decrypted data: %v", err)
				}

				if !bytes.Equal(decrypted, cleartext) {
					t.Error("decrypted output does not match original cleartext")
				}
			default:
				t.Logf("No OpenSSL validation for: %s", encryptor.Name())
			}
		})
	}
}

func generateRandomCleartext(size int) ([]byte, error) {
	buf := make([]byte, size)
	_, err := rand.Read(buf)
	return buf, err
}

func generateRandomKey(size int) ([]byte, error) {
	key := make([]byte, size)
	_, err := rand.Read(key)
	return key, err
}

func generateSelfSignedCert() (certPEM, keyPEM []byte, err error) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "Test SMIME Cert"},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageEmailProtection},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privKey.PublicKey, privKey)
	if err != nil {
		return nil, nil, err
	}

	certPEMBuffer := new(bytes.Buffer)
	if err := pem.Encode(certPEMBuffer, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		return nil, nil, err
	}

	keyPEMBuffer := new(bytes.Buffer)
	privBytes := x509.MarshalPKCS1PrivateKey(privKey)
	if err := pem.Encode(keyPEMBuffer, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: privBytes}); err != nil {
		return nil, nil, err
	}

	return certPEMBuffer.Bytes(), keyPEMBuffer.Bytes(), nil
}
