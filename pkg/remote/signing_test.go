package remote

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/databacker/api/go/api"

	"github.com/databacker/mysql-backup/pkg/identity"
)

type captureTransport struct{ request *http.Request }

func (transport *captureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.request = request
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: request}, nil
}

func signingIdentity(t *testing.T) *identity.Identity {
	t.Helper()
	engineIdentity, err := identity.New(api.EngineCredentials{
		Version:                           api.DatabackerCredentialsV2,
		Seed:                              "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=",
		AuthenticationGeneration:          1,
		ConfigurationEncryptionGeneration: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return engineIdentity
}

func TestSigningFixtureGET(t *testing.T) {
	capture := &captureTransport{}
	transport, err := NewSigningTransport(capture, signingIdentity(t))
	if err != nil {
		t.Fatal(err)
	}
	transport.Now = func() time.Time { return time.Unix(1700000000, 0) }
	transport.Random = bytes.NewReader([]byte{0xa0, 0xa1, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7, 0xa8, 0xa9, 0xaa, 0xab, 0xac, 0xad, 0xae, 0xaf})
	request, _ := http.NewRequest(http.MethodGet, "https://engine.example/engines/config", nil)
	if _, err := transport.RoundTrip(request); err != nil {
		t.Fatal(err)
	}
	wantInput := `databacker-engine=("@method" "@authority" "@path" "@query");created=1700000000;expires=1700000300;keyid="auth:10ae5b498547bbbb760358b0f2ee659ccbf17e8858263eff0c06225c6ebbaf3e";nonce="oKGio6SlpqeoqaqrrK2urw";tag="databacker-engine-v1"`
	if got := capture.request.Header.Get("Signature-Input"); got != wantInput {
		t.Fatalf("Signature-Input:\n got %s\nwant %s", got, wantInput)
	}
	wantSignature := "databacker-engine=:UBqMiyDjEJzkrrkyhBk9HM5F/zRt4+K3b0eGhsDWAEqq99SFg/0f3RI1wog2ImnOUl6eareKtyHvaEyWUl1uAA==:"
	if got := capture.request.Header.Get("Signature"); got != wantSignature {
		t.Fatalf("Signature = %q, want %q", got, wantSignature)
	}
}

func TestSigningFixtureBody(t *testing.T) {
	capture := &captureTransport{}
	transport, _ := NewSigningTransport(capture, signingIdentity(t))
	transport.Now = func() time.Time { return time.Unix(1700000000, 0) }
	transport.Random = bytes.NewReader([]byte{0xa0, 0xa1, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7, 0xa8, 0xa9, 0xaa, 0xab, 0xac, 0xad, 0xae, 0xaf})
	request, _ := http.NewRequest(http.MethodPost, "https://engine.example/engines/telemetry/traces?format=otlp&part=one&part=two", bytes.NewReader([]byte{0, 1, 2, 3, 4, 5}))
	request.Header.Set("Content-Type", "application/x-protobuf")
	request.Header.Set("Idempotency-Key", "550e8400-e29b-41d4-a716-446655440000")
	if _, err := transport.RoundTrip(request); err != nil {
		t.Fatal(err)
	}
	wantSignature := "databacker-engine=:cv7VymK8EtPIUGh6Qms+d4C6QNhrl4hmlhcxgLcZs/AwXhb9i8r3LMrzDPHEfliXhnRNOQQjGXY1w1vqNUQ+Dg==:"
	if got := capture.request.Header.Get("Signature"); got != wantSignature {
		t.Fatalf("Signature = %q, want %q", got, wantSignature)
	}
	digest := sha256.Sum256([]byte{0, 1, 2, 3, 4, 5})
	wantDigest := "sha-256=:" + base64.StdEncoding.EncodeToString(digest[:]) + ":"
	if got := capture.request.Header.Get("Content-Digest"); got != wantDigest {
		t.Fatalf("Content-Digest = %q, want %q", got, wantDigest)
	}
}

func TestRegistrationSigningFixture(t *testing.T) {
	body := `{"name":"fixture-engine","description":"Fixture registration","publicKeys":{"authentication":{"algorithm":"ed25519","generation":1,"publicKey":"bCfocojhXze28IS3hlHnttC+OO469HweOvPruf3q8wY="},"configurationEncryption":{"algorithm":"x25519","generation":1,"publicKey":"XwG2fwzzSPppHov/SmmQ2SlsfbgAqscGd+UztjrLOEo="}}}`
	request, _ := http.NewRequest(http.MethodPost, "https://cloud.example/admin/accounts/account-123/engines", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "550e8400-e29b-41d4-a716-446655440000")
	nonce := bytes.NewReader([]byte{0xa0, 0xa1, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7, 0xa8, 0xa9, 0xaa, 0xab, 0xac, 0xad, 0xae, 0xaf})
	if err := signRegistrationRequest(request, signingIdentity(t), time.Unix(1700000000, 0), nonce); err != nil {
		t.Fatal(err)
	}
	if got, want := request.Header.Get("Content-Digest"), "sha-256=:Dbgi/FOEu12LsF3Mu/XvGHSSIgqrx/lxp8i0QpuFF6Y=:"; got != want {
		t.Fatalf("Content-Digest = %q, want %q", got, want)
	}
	wantInput := `databacker-engine=("@method" "@authority" "@path" "@query" "content-digest";sf "content-type" "idempotency-key");created=1700000000;expires=1700000300;keyid="auth:10ae5b498547bbbb760358b0f2ee659ccbf17e8858263eff0c06225c6ebbaf3e";nonce="oKGio6SlpqeoqaqrrK2urw";tag="databacker-engine-registration-v1"`
	if got := request.Header.Get("Signature-Input"); got != wantInput {
		t.Fatalf("Signature-Input:\n got %s\nwant %s", got, wantInput)
	}
	wantSignature := "databacker-engine=:dUmpW++ljDtcSfLr1BojHFEdmqFazPjPVqgq4qumF0k2K+dMiiQZWKnhwiHETlLXHeRf/y6pH0PPqMYVy8wIAw==:"
	if got := request.Header.Get("Signature"); got != wantSignature {
		t.Fatalf("Signature = %q, want %q", got, wantSignature)
	}
}

func TestSigningRejectsUnsupportedRouteAndEncoding(t *testing.T) {
	transport, _ := NewSigningTransport(&captureTransport{}, signingIdentity(t))
	request, _ := http.NewRequest(http.MethodGet, "https://engine.example/engines/config/other", nil)
	if _, err := transport.RoundTrip(request); err == nil {
		t.Fatal("expected unsupported route to fail")
	}
	request, _ = http.NewRequest(http.MethodPost, "https://engine.example/engines/telemetry/traces", strings.NewReader("body"))
	request.Header.Set("Content-Type", "application/x-protobuf")
	request.Header.Set("Content-Encoding", "gzip")
	if _, err := transport.RoundTrip(request); err == nil {
		t.Fatal("expected compressed request to fail")
	}
	request, _ = http.NewRequest(http.MethodPost, "https://engine.example/engines/telemetry/traces", strings.NewReader("body"))
	request.Header.Set("Content-Type", "application/x-protobuf")
	request.Header.Set("Idempotency-Key", "not-a-uuid")
	if _, err := transport.RoundTrip(request); err == nil {
		t.Fatal("expected invalid idempotency key to fail")
	}
}

func TestSigningRetryPreservesIdempotencyAndRefreshesSignature(t *testing.T) {
	capture := &captureTransport{}
	transport, _ := NewSigningTransport(capture, signingIdentity(t))
	now := time.Unix(1700000000, 0)
	transport.Now = func() time.Time { current := now; now = now.Add(time.Second); return current }
	transport.Random = bytes.NewReader(append(make([]byte, 16), bytes.Repeat([]byte{1}, 16)...))
	request, _ := http.NewRequest(http.MethodPost, "https://engine.example/engines/telemetry/traces", strings.NewReader("body"))
	request.Header.Set("Content-Type", "application/x-protobuf")
	if _, err := transport.RoundTrip(request); err != nil {
		t.Fatal(err)
	}
	idempotencyKey := request.Header.Get("Idempotency-Key")
	firstSignature := capture.request.Header.Get("Signature")
	request.Body, _ = request.GetBody()
	if _, err := transport.RoundTrip(request); err != nil {
		t.Fatal(err)
	}
	if request.Header.Get("Idempotency-Key") != idempotencyKey {
		t.Fatal("idempotency key changed across retry")
	}
	if capture.request.Header.Get("Signature") == firstSignature {
		t.Fatal("signature was not refreshed across retry")
	}
}

func TestSigningFailsClosed(t *testing.T) {
	tests := []struct {
		name    string
		request func() *http.Request
		adjust  func(*SigningTransport)
	}{
		{
			name: "unsupported scheme",
			request: func() *http.Request {
				req, _ := http.NewRequest(http.MethodGet, "ftp://engine.example/engines/config", nil)
				return req
			},
		},
		{
			name: "missing content type",
			request: func() *http.Request {
				req, _ := http.NewRequest(http.MethodPost, "https://engine.example/engines/telemetry/traces", strings.NewReader("body"))
				return req
			},
		},
		{
			name: "oversized body",
			request: func() *http.Request {
				req, _ := http.NewRequest(http.MethodPost, "https://engine.example/engines/telemetry/traces", strings.NewReader("body"))
				req.Header.Set("Content-Type", "application/x-protobuf")
				return req
			},
			adjust: func(transport *SigningTransport) { transport.MaxBody = 3 },
		},
		{
			name: "nonce failure",
			request: func() *http.Request {
				req, _ := http.NewRequest(http.MethodGet, "https://engine.example/engines/config", nil)
				return req
			},
			adjust: func(transport *SigningTransport) { transport.Random = strings.NewReader("") },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport, _ := NewSigningTransport(&captureTransport{}, signingIdentity(t))
			if test.adjust != nil {
				test.adjust(transport)
			}
			if _, err := transport.RoundTrip(test.request()); err == nil {
				t.Fatal("expected request to fail")
			}
		})
	}
}

func TestSigningAllowsPlainHTTP(t *testing.T) {
	capture := &captureTransport{}
	transport, _ := NewSigningTransport(capture, signingIdentity(t))
	request, _ := http.NewRequest(http.MethodGet, "http://engine.example/engines/config", nil)
	if _, err := transport.RoundTrip(request); err != nil {
		t.Fatal(err)
	}
	if capture.request.Header.Get("Signature") == "" {
		t.Fatal("HTTP request was not signed")
	}
}

func TestSignedClientRefusesRedirects(t *testing.T) {
	client, err := NewSignedClient(nil, signingIdentity(t))
	if err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest(http.MethodGet, "https://engine.example/engines/config", nil)
	if err := client.CheckRedirect(request, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("redirect error = %v", err)
	}
}

func TestResolveEngineEndpoint(t *testing.T) {
	tests := []struct {
		name  string
		base  string
		route EngineRoute
		want  string
	}{
		{name: "host base", base: "https://engine.example", route: ConfigRoute, want: "https://engine.example/engines/config"},
		{name: "deployment prefix", base: "http://engine.example/v1/", route: TelemetryTracesRoute, want: "http://engine.example/v1/engines/telemetry/traces"},
		{name: "complete route", base: "https://engine.example/engines/config", route: ConfigRoute, want: "https://engine.example/engines/config"},
		{name: "complete route trailing slash", base: "https://engine.example/engines/config/", route: ConfigRoute, want: "https://engine.example/engines/config"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ResolveEngineEndpoint(test.base, test.route)
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != test.want {
				t.Fatalf("endpoint = %q, want %q", got, test.want)
			}
		})
	}
}

func TestResolveEngineEndpointRejectsInvalidBase(t *testing.T) {
	tests := []string{
		"engine.example",
		"ftp://engine.example",
		"https://engine.example?tenant=one",
		"https://engine.example#fragment",
		"https://user@engine.example",
		"https://engine.example/engines/telemetry/log",
	}
	for _, base := range tests {
		if _, err := ResolveEngineEndpoint(base, ConfigRoute); err == nil {
			t.Fatalf("expected %q to fail", base)
		}
	}
}

func TestSigningAllowsDeploymentPrefix(t *testing.T) {
	capture := &captureTransport{}
	transport, _ := NewSigningTransport(capture, signingIdentity(t))
	request, _ := http.NewRequest(http.MethodGet, "https://engine.example/v1/engines/config", nil)
	if _, err := transport.RoundTrip(request); err != nil {
		t.Fatal(err)
	}
}
