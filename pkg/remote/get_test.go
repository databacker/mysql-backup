package remote

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPinnedTLSFallbackSendsNoClientCertificate(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if len(request.TLS.PeerCertificates) != 0 {
			t.Error("client sent a certificate")
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	digest := sha256.Sum256(server.Certificate().Raw)
	transport, err := NewTransport([]string{fmt.Sprintf("sha256:%x", digest)})
	if err != nil {
		t.Fatal(err)
	}
	response, err := (&http.Client{Transport: transport}).Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", response.StatusCode)
	}
}

func TestTLSRejectsUntrustedAndMalformedPins(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	transport, err := NewTransport(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&http.Client{Transport: transport}).Get(server.URL); err == nil {
		t.Fatal("expected system verification to reject self-signed server")
	}
	for _, pin := range []string{"", "md5:00", "sha256:not-hex", "sha256:00"} {
		if _, err := NewTransport([]string{pin}); err == nil {
			t.Fatalf("expected malformed pin %q to fail", pin)
		}
	}
}

func TestPinnedTLSStillChecksHostname(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	digest := sha256.Sum256(server.Certificate().Raw)
	transport, err := NewTransport([]string{fmt.Sprintf("sha256:%x", digest)})
	if err != nil {
		t.Fatal(err)
	}
	transport.TLSClientConfig.ServerName = "wrong.example"
	if _, err := (&http.Client{Transport: transport}).Get(server.URL); err == nil {
		t.Fatal("expected matching pin with wrong hostname to fail")
	}
}
