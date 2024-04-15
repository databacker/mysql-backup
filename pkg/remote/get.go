package remote

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"strings"
	"time"
)

var (
	validAlgos     = []string{digestSha256}
	validAlgosHash = map[string]bool{}
)

func init() {
	for _, algo := range validAlgos {
		validAlgosHash[algo] = true
	}
}

// OpenConnection opens a connection to a TLS server, given the URL, digests of acceptable certs, and curve25519 key for authentication.
// The credentials should be base64-encoded curve25519 private key. This is curve25519 and *not* ed25519; ed25519 calls this
// the "seed key". It must be 32 bytes long.
// The certs should be a list of fingerprints in the format "algo:hex-fingerprint".
func OpenConnection(u string, certs []string, credentials string) (resp *http.Response, err error) {
	// open a connection to the URL.
	// Uses mTLS, but rather than verifying the CA that signed the client cert,
	// server should accept a self-signed cert. It then should check if the client's public key is in a known good list.

	var trustedCertsByAlgo = map[string]map[string]bool{}
	for _, fingerprint := range certs {
		parts := strings.SplitN(fingerprint, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid fingerprint format from configuration: %s", fingerprint)
		}
		algo, fp := parts[0], parts[1]
		if !validAlgosHash[algo] {
			return nil, fmt.Errorf("invalid algorithm in fingerprint: %s", fingerprint)
		}
		if trustedCertsByAlgo[algo] == nil {
			trustedCertsByAlgo[algo] = map[string]bool{}
		}
		trustedCertsByAlgo[algo][fp] = true
	}
	// get our curve25519 key
	keyBytes, err := base64.StdEncoding.DecodeString(credentials)
	if err != nil {
		return nil, fmt.Errorf("error decoding credentials: %w", err)
	}
	if len(keyBytes) != ed25519.SeedSize {
		return nil, fmt.Errorf("invalid key size %d, must be %d", len(keyBytes), ed25519.SeedSize)
	}

	key := ed25519.NewKeyFromSeed(keyBytes)
	clientCert, err := selfSignedCertFromPrivateKey(key, "")
	if err != nil {
		return nil, fmt.Errorf("error creating client certificate: %w", err)
	}

	client := &http.Client{
		Transport: &http.Transport{
			// Configure TLS via DialTLS
			DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				tlsConfig := &tls.Config{
					InsecureSkipVerify: true, // disable regular verification, because go has no way to do regular verification and only fallback to my function
					VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
						// If verifiedChains is not empty, then the normal verification has passed.
						if len(verifiedChains) > 0 {
							return nil
						}

						// get the address part of addr
						host, _, err := net.SplitHostPort(addr)
						if err != nil {
							return fmt.Errorf("failed to parse address: %v", err)
						}

						certs := make([]*x509.Certificate, len(rawCerts))
						for i, asn1Data := range rawCerts {
							cert, err := x509.ParseCertificate(asn1Data)
							if err != nil {
								return fmt.Errorf("failed to parse certificate: %v", err)
							}
							certs[i] = cert
						}

						// Try to verify the certificate chain using the system pool
						opts := x509.VerifyOptions{
							Intermediates: x509.NewCertPool(),
							DNSName:       host,
						}
						for i, cert := range certs {
							// skip the first cert, because it's the one we're trying to verify
							if i == 0 {
								continue
							}
							// add every other cert as a valid intermediate
							opts.Intermediates.AddCert(cert)
						}

						// if one of the certs is valid and verified, accept it
						if _, err := certs[0].Verify(opts); err == nil {
							return nil
						}

						// the cert presented by the server was not signed by a known CA, so fall back to our own list
						for _, rawCert := range rawCerts {
							fingerprint := fmt.Sprintf("%x", sha256.Sum256(rawCert))
							if trustedFingerprints, ok := trustedCertsByAlgo[digestSha256]; ok {
								if _, ok := trustedFingerprints[fingerprint]; ok {
									if validateCert(certs[0], host) {
										return nil
									}
								}
							}
						}

						// not in system or in the approved list
						return fmt.Errorf("certificate not trusted")
					},
					Certificates: []tls.Certificate{*clientCert},
				}
				return tls.Dial(network, addr, tlsConfig)
			},
		},
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating HTTP request: %w", err)
	}

	return client.Do(req)
}

// selfSignedCertFromPrivateKey creates a self-signed certificate from an ed25519 private key
func selfSignedCertFromPrivateKey(privateKey ed25519.PrivateKey, hostname string) (*tls.Certificate, error) {
	if privateKey == nil || len(privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid private key")
	}
	publicKey := privateKey.Public()

	// Create a template for the certificate
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{clientOrg},
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(certValidity),

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	if hostname != "" {
		template.DNSNames = append(template.DNSNames, hostname)
	}

	// Self-sign the certificate
	certBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, publicKey, privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	// Encode and print the certificate
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certBytes})

	// Create the TLS certificate to use in tls.Config
	marshaledPrivateKey, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal private key: %w", err)
	}
	cert, err := tls.X509KeyPair(certPEM, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: marshaledPrivateKey}))
	return &cert, err
}

// validateCert given a cert that we decided to trust its cert or signature, make sure its properties are correct:
// - still valid expiration date
// - hostname matches
// - valid function
func validateCert(cert *x509.Certificate, hostname string) bool {
	// valid date
	now := time.Now()
	if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
		return false
	}

	// valid hostname or IP
	var validHostname bool
	for _, dnsName := range cert.DNSNames {
		if dnsName == hostname {
			validHostname = true
			break
		}
	}
	if hostname == cert.Subject.CommonName {
		validHostname = true
	}
	if !validHostname {
		return false
	}

	// check keyusage
	if cert.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
		return false
	}
	return true
}
