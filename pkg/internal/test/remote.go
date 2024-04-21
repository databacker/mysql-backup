package test

import (
	"crypto"
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	utilremote "github.com/databacker/mysql-backup/pkg/internal/remote"
)

func StartServer(clientKeyCount int, handler http.HandlerFunc) (server *httptest.Server, serverFingerprint string, clientKeys [][]byte, err error) {
	// Generate new private keys for each of the clients
	var clientPublicKeys []crypto.PublicKey
	for i := 0; i < clientKeyCount; i++ {
		clientSeed := make([]byte, ed25519.SeedSize)
		if _, err := io.ReadFull(cryptorand.Reader, clientSeed); err != nil {
			return nil, "", nil, fmt.Errorf("failed to generate client random seed: %w", err)
		}
		clientKeys = append(clientKeys, clientSeed)
		clientKey := ed25519.NewKeyFromSeed(clientSeed)
		clientPublicKeys = append(clientPublicKeys, clientKey.Public())
	}

	serverSeed := make([]byte, ed25519.SeedSize)
	if _, err := io.ReadFull(cryptorand.Reader, serverSeed); err != nil {
		return nil, "", nil, fmt.Errorf("failed to generate server random seed: %w", err)
	}
	serverKey := ed25519.NewKeyFromSeed(serverSeed)

	// Create a self-signed certificate from the private key
	serverCert, err := utilremote.SelfSignedCertFromPrivateKey(serverKey, "127.0.0.1")
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to create self-signed certificate: %v", err)
	}
	serverFingerprint = fmt.Sprintf("%s:%s", utilremote.DigestSha256, fmt.Sprintf("%x", sha256.Sum256(serverCert.Certificate[0])))

	// Start a local HTTPS server
	server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if the client's public key is in the known list
		peerCerts := r.TLS.PeerCertificates
		if len(peerCerts) == 0 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		peerPublicKey := peerCerts[0].PublicKey.(ed25519.PublicKey)
		// make sure the client's public key is in the list of known keys
		var matched bool
		for _, publicKey := range clientPublicKeys {
			if peerPublicKey.Equal(publicKey.(ed25519.PublicKey)) {
				matched = true
				break
			}
		}
		if !matched {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		// was any custom handler passed?
		if handler != nil {
			handler(w, r)
		}
	}))
	server.TLS = &tls.Config{
		ClientAuth:   tls.RequestClientCert,
		ClientCAs:    x509.NewCertPool(),
		Certificates: []tls.Certificate{*serverCert},
	}
	server.StartTLS()
	return server, serverFingerprint, clientKeys, nil
}
