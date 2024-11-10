package remote

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	utilremote "github.com/databacker/mysql-backup/pkg/internal/remote"
)

var (
	validAlgos     = []string{utilremote.DigestSha256}
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
func OpenConnection(urlString string, certs []string, credentials string) (resp *http.Response, err error) {
	// open a connection to the URL.
	// Uses mTLS, but rather than verifying the CA that signed the client cert,
	// server should accept a self-signed cert. It then should check if the client's public key is in a known good list.
	u, err := url.Parse(urlString)
	if err != nil {
		return nil, fmt.Errorf("error parsing URL: %w", err)
	}
	client, err := GetTLSClient(u.Hostname(), certs, credentials)
	if err != nil {
		return nil, fmt.Errorf("error creating TLS client: %w", err)
	}

	req, err := http.NewRequest(http.MethodGet, urlString, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating HTTP request: %w", err)
	}

	return client.Do(req)
}

// GetTLSClient gets a TLS client for a connection to a TLS server, given the URL, digests of acceptable certs, and curve25519 key for authentication.
// The credentials should be base64-encoded curve25519 private key. This is curve25519 and *not* ed25519; ed25519 calls this
// the "seed key". It must be 32 bytes long.
// The certs should be a list of fingerprints in the format "algo:hex-fingerprint".
func GetTLSClient(serverName string, certs []string, credentials string) (client *http.Client, err error) {
	tlsConfig, err := GetTLSConfig(serverName, certs, credentials)
	if err != nil {
		return nil, fmt.Errorf("error creating TLS config: %w", err)
	}
	client = &http.Client{
		Transport: &http.Transport{
			// Configure TLS via DialTLS
			DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return tls.Dial(network, addr, tlsConfig)
			},
		},
	}
	return client, nil
}

// GetTLSConfig gets the TLS config a TLS client for a connection to a TLS server, given the digests of acceptable certs, and curve25519 key for authentication.
// The credentials should be base64-encoded curve25519 private key. This is curve25519 and *not* ed25519; ed25519 calls this
// the "seed key". It must be 32 bytes long.
// The certs should be a list of fingerprints in the format "algo:hex-fingerprint".
func GetTLSConfig(serverName string, certs []string, credentials string) (tlsConfig *tls.Config, err error) {
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
	clientCert, err := utilremote.SelfSignedCertFromPrivateKey(key, "")
	if err != nil {
		return nil, fmt.Errorf("error creating client certificate: %w", err)
	}

	tlsConfig = &tls.Config{
		ServerName:         serverName,
		InsecureSkipVerify: true, // disable regular verification, because go has no way to do regular verification and only fallback to my function
		VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			// If verifiedChains is not empty, then the normal verification has passed.
			if len(verifiedChains) > 0 {
				return nil
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
				DNSName:       tlsConfig.ServerName,
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
				if trustedFingerprints, ok := trustedCertsByAlgo[utilremote.DigestSha256]; ok {
					if _, ok := trustedFingerprints[fingerprint]; ok {
						if validateCert(certs[0], tlsConfig.ServerName) {
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
	return tlsConfig, nil
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
