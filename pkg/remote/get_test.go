package remote

import (
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"io"
	"net/http"
	"testing"

	utilremote "github.com/databacker/mysql-backup/pkg/internal/remote"
	utiltest "github.com/databacker/mysql-backup/pkg/internal/test"
)

func TestSelfSignedCertFromPrivateKey(t *testing.T) {
	// Generate a new private key
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("failed to generate private key: %v", err)
	}

	tests := []struct {
		name        string
		privateKey  ed25519.PrivateKey
		expectError bool
	}{
		{
			name:        "valid private key",
			privateKey:  privateKey,
			expectError: false,
		},
		{
			name:        "nil private key",
			privateKey:  nil,
			expectError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Call the function with the private key
			cert, err := utilremote.SelfSignedCertFromPrivateKey(test.privateKey, "")
			if (err != nil) != test.expectError {
				t.Fatalf("selfSignedCertFromPrivateKey returned an error: %v", err)
			}

			if !test.expectError {
				// Check if the returned certificate is not nil
				if cert == nil {
					t.Fatalf("selfSignedCertFromPrivateKey returned a nil certificate")
				}

				// Parse the certificate
				parsedCert, err := x509.ParseCertificate(cert.Certificate[0])
				if err != nil {
					t.Fatalf("failed to parse certificate: %v", err)
				}

				// Check if the certificate's public key matches the private key's public key
				if !publicKey.Equal(parsedCert.PublicKey.(ed25519.PublicKey)) {
					t.Fatalf("public key in certificate does not match private key's public key")
				}
			}
		})
	}
}

func TestOpenConnection(t *testing.T) {
	// Generate a private key that is not in the list of known keys
	clientSeedUnknown := make([]byte, ed25519.SeedSize)
	if _, err := io.ReadFull(cryptorand.Reader, clientSeedUnknown); err != nil {
		t.Fatalf("failed to generate random seed: %v", err)
	}
	server, fingerprint, clientKeys, err := utiltest.StartServer(1, nil)
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Close()

	tests := []struct {
		name             string
		clientPrivateKey []byte
		certs            []string
		expectError      bool
		expectedStatus   int
	}{
		{
			name:             "client key in list",
			clientPrivateKey: clientKeys[0],
			certs:            []string{fingerprint},
			expectError:      false,
			expectedStatus:   http.StatusOK,
		},
		{
			name:             "client key not in list",
			clientPrivateKey: clientSeedUnknown,
			certs:            []string{fingerprint},
			expectError:      false,
			expectedStatus:   http.StatusForbidden,
		},
		{
			name:             "no certs",
			clientPrivateKey: clientKeys[0],
			certs:            []string{},
			expectError:      true,
			expectedStatus:   http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call openConnection
			b64EncodedClientKey := base64.StdEncoding.EncodeToString(tt.clientPrivateKey)
			resp, err := OpenConnection(server.URL, tt.certs, b64EncodedClientKey)
			switch {
			case err != nil && !tt.expectError:
				t.Errorf("openConnection returned an unexpected error: %v", err)
			case err == nil && tt.expectError:
				t.Errorf("openConnection did not return an expected error: %v", err)
			case err == nil && resp.StatusCode != tt.expectedStatus:
				t.Errorf("openConnection returned an unexpected status code: %d", resp.StatusCode)
			}
		})
	}
}
