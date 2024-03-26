package remote

import (
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
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
			cert, err := selfSignedCertFromPrivateKey(test.privateKey, "")
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
	// Generate a new private key
	clientSeed1 := make([]byte, ed25519.SeedSize)
	if _, err := io.ReadFull(cryptorand.Reader, clientSeed1); err != nil {
		t.Fatalf("failed to generate random seed: %v", err)
	}
	clientKey1 := ed25519.NewKeyFromSeed(clientSeed1)

	clientSeed2 := make([]byte, ed25519.SeedSize)
	if _, err := io.ReadFull(cryptorand.Reader, clientSeed2); err != nil {
		t.Fatalf("failed to generate random seed: %v", err)
	}

	serverSeed := make([]byte, ed25519.SeedSize)
	if _, err := io.ReadFull(cryptorand.Reader, serverSeed); err != nil {
		t.Fatalf("failed to generate random seed: %v", err)
	}
	serverKey := ed25519.NewKeyFromSeed(serverSeed)

	// Create a self-signed certificate from the private key
	serverCert, err := selfSignedCertFromPrivateKey(serverKey, "127.0.0.1")
	if err != nil {
		t.Fatalf("failed to create self-signed certificate: %v", err)
	}
	fingerprint := fmt.Sprintf("%s:%s", digestSha256, fmt.Sprintf("%x", sha256.Sum256(serverCert.Certificate[0])))

	// Start a local HTTPS server
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if the client's public key is in the known list
		peerCerts := r.TLS.PeerCertificates
		if len(peerCerts) == 0 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		peerPublicKey := peerCerts[0].PublicKey.(ed25519.PublicKey)
		expectedPublicKey := clientKey1.Public().(ed25519.PublicKey)
		if !peerPublicKey.Equal(expectedPublicKey) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
	}))
	server.TLS = &tls.Config{
		ClientAuth:   tls.RequestClientCert,
		ClientCAs:    x509.NewCertPool(),
		Certificates: []tls.Certificate{*serverCert},
	}
	server.StartTLS()
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
			clientPrivateKey: clientSeed1,
			certs:            []string{fingerprint},
			expectError:      false,
			expectedStatus:   http.StatusOK,
		},
		{
			name:             "client key not in list",
			clientPrivateKey: clientSeed2,
			certs:            []string{fingerprint},
			expectError:      false,
			expectedStatus:   http.StatusForbidden,
		},
		{
			name:             "no certs",
			clientPrivateKey: clientSeed1,
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
