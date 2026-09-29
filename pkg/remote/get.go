package remote

import (
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// NewTransport constructs an ordinary HTTP transport. For HTTPS requests,
// configured certificate fingerprints are used only as a fallback when Web
// PKI verification fails.
func NewTransport(fingerprints []string) (*http.Transport, error) {
	pins, err := parseFingerprints(fingerprints)
	if err != nil {
		return nil, err
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if len(pins) > 0 {
		tlsConfig.InsecureSkipVerify = true
		tlsConfig.VerifyConnection = func(state tls.ConnectionState) error {
			return verifyConnection(state, pins)
		}
	}
	return &http.Transport{Proxy: http.ProxyFromEnvironment, TLSClientConfig: tlsConfig}, nil
}

// NewClient creates an unsigned HTTP client that never follows redirects.
func NewClient(fingerprints []string) (*http.Client, error) {
	transport, err := NewTransport(fingerprints)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Transport:     transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}, nil
}

func parseFingerprints(values []string) ([][]byte, error) {
	pins := make([][]byte, 0, len(values))
	for _, value := range values {
		algorithm, encoded, ok := strings.Cut(value, ":")
		if !ok || algorithm != "sha256" || encoded == "" {
			return nil, fmt.Errorf("invalid server certificate fingerprint")
		}
		decoded, err := hex.DecodeString(encoded)
		if err != nil || len(decoded) != sha256.Size {
			return nil, fmt.Errorf("invalid server certificate fingerprint")
		}
		pins = append(pins, decoded)
	}
	return pins, nil
}

func verifyConnection(state tls.ConnectionState, pins [][]byte) error {
	if len(state.PeerCertificates) == 0 {
		return errors.New("remote server presented no certificate")
	}
	opts := x509.VerifyOptions{
		DNSName:       state.ServerName,
		Intermediates: x509.NewCertPool(),
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, certificate := range state.PeerCertificates[1:] {
		opts.Intermediates.AddCert(certificate)
	}
	if _, err := state.PeerCertificates[0].Verify(opts); err == nil {
		return nil
	}

	pinnedRoots := x509.NewCertPool()
	matched := false
	for _, certificate := range state.PeerCertificates {
		digest := sha256.Sum256(certificate.Raw)
		for _, pin := range pins {
			if subtle.ConstantTimeCompare(digest[:], pin) == 1 {
				pinnedRoots.AddCert(certificate)
				matched = true
			}
		}
	}
	if !matched {
		return errors.New("remote server certificate is not trusted")
	}
	opts.Roots = pinnedRoots
	if _, err := state.PeerCertificates[0].Verify(opts); err != nil {
		return errors.New("pinned remote server certificate failed validation")
	}
	return nil
}
