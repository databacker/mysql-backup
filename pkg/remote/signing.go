package remote

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/databacker/mysql-backup/pkg/identity"
)

const (
	signatureLabel  = "databacker-engine"
	signatureTag    = "databacker-engine-v1"
	registrationTag = "databacker-engine-registration-v1"
	defaultMaxBody  = 8 << 20
)

// EngineRoute is a self-only southbound API route. The authenticated signing
// key identifies the engine; no Cloud-assigned engine ID appears in the URL.
type EngineRoute string

const (
	ConfigRoute          EngineRoute = "/engines/config"
	TelemetryLogRoute    EngineRoute = "/engines/telemetry/log"
	TelemetryTracesRoute EngineRoute = "/engines/telemetry/traces"
)

// SigningTransport applies the Databacker RFC 9421 profile to engine requests.
type SigningTransport struct {
	Base     http.RoundTripper
	Identity *identity.Identity
	Now      func() time.Time
	Random   io.Reader
	MaxBody  int64
}

// NewSigningTransport constructs a signing transport over base.
func NewSigningTransport(base http.RoundTripper, engineIdentity *identity.Identity) (*SigningTransport, error) {
	if base == nil || engineIdentity == nil {
		return nil, errors.New("signing transport requires a base transport and identity")
	}
	return &SigningTransport{Base: base, Identity: engineIdentity}, nil
}

// NewSignedClient constructs the production HTTP client for engine requests.
func NewSignedClient(fingerprints []string, engineIdentity *identity.Identity) (*http.Client, error) {
	base, err := NewTransport(fingerprints)
	if err != nil {
		return nil, err
	}
	signer, err := NewSigningTransport(base, engineIdentity)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Transport:     signer,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}, nil
}

func (transport *SigningTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL == nil || request.URL.Host == "" || request.URL.Scheme != "http" && request.URL.Scheme != "https" {
		return nil, errors.New("signed engine requests require an absolute HTTP or HTTPS URL")
	}
	if !isEngineRoute(request.URL.Path) {
		return nil, errors.New("signed request path is not a supported self-only engine route")
	}
	encodings := request.Header.Values("Content-Encoding")
	if len(encodings) > 1 || len(encodings) == 1 && !strings.EqualFold(encodings[0], "identity") {
		return nil, errors.New("signed request uses unsupported content encoding")
	}

	hasBody := request.Body != nil && request.Body != http.NoBody
	if hasBody && request.Header.Get("Idempotency-Key") == "" {
		// The OTLP exporter retries the same request object with a reset body.
		// Persist the logical-operation key on that request while signatures are
		// produced on a clone for each individual HTTP attempt.
		request.Header.Set("Idempotency-Key", uuid.NewString())
	}
	if hasBody {
		idempotencyKeys := request.Header.Values("Idempotency-Key")
		if len(idempotencyKeys) != 1 {
			return nil, errors.New("body-bearing signed request requires one Idempotency-Key")
		}
		if parsed, err := uuid.Parse(idempotencyKeys[0]); err != nil || parsed.String() != idempotencyKeys[0] {
			return nil, errors.New("body-bearing signed request requires a canonical UUID Idempotency-Key")
		}
	}
	signed := request.Clone(request.Context())
	signed.Header = request.Header.Clone()
	if hasBody {
		limit := transport.MaxBody
		if limit <= 0 {
			limit = defaultMaxBody
		}
		body, err := io.ReadAll(io.LimitReader(request.Body, limit+1))
		if err != nil {
			return nil, errors.New("failed to read request body for signing")
		}
		if int64(len(body)) > limit {
			return nil, errors.New("request body exceeds signing limit")
		}
		_ = request.Body.Close()
		request.Body = io.NopCloser(bytes.NewReader(body))
		signed.Body = io.NopCloser(bytes.NewReader(body))
		signed.ContentLength = int64(len(body))
		signed.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
		if len(signed.Header.Values("Content-Type")) != 1 || signed.Header.Get("Content-Type") == "" {
			return nil, errors.New("body-bearing signed request requires one Content-Type")
		}
		digest := sha256.Sum256(body)
		signed.Header.Set("Content-Digest", "sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":")
	}

	now := transport.Now
	if now == nil {
		now = time.Now
	}
	random := transport.Random
	if random == nil {
		random = rand.Reader
	}
	if err := applySignature(signed, transport.Identity, hasBody, signatureTag, now(), random); err != nil {
		return nil, err
	}

	return transport.Base.RoundTrip(signed)
}

// SignRegistrationRequest applies the registration proof profile to a POST or
// PATCH request. The caller must transmit the same request body and headers.
func SignRegistrationRequest(request *http.Request, engineIdentity *identity.Identity) error {
	return signRegistrationRequest(request, engineIdentity, time.Now(), rand.Reader)
}

func signRegistrationRequest(request *http.Request, engineIdentity *identity.Identity, now time.Time, random io.Reader) error {
	if request == nil || request.URL == nil || request.URL.Host == "" || request.URL.Scheme != "http" && request.URL.Scheme != "https" {
		return errors.New("registration proof requires an absolute HTTP or HTTPS URL")
	}
	if engineIdentity == nil {
		return errors.New("registration proof requires an engine identity")
	}
	if request.Method != http.MethodPost && request.Method != http.MethodPatch {
		return errors.New("registration proof requires POST or PATCH")
	}
	if request.Body == nil || request.Body == http.NoBody {
		return errors.New("registration proof requires a request body")
	}
	if len(request.Header.Values("Content-Type")) != 1 || request.Header.Get("Content-Type") != "application/json" {
		return errors.New("registration proof requires Content-Type application/json")
	}
	if values := request.Header.Values("Content-Encoding"); len(values) > 1 || len(values) == 1 && !strings.EqualFold(values[0], "identity") {
		return errors.New("registration proof uses unsupported content encoding")
	}
	idempotencyKeys := request.Header.Values("Idempotency-Key")
	if len(idempotencyKeys) != 1 {
		return errors.New("registration proof requires one Idempotency-Key")
	}
	if parsed, err := uuid.Parse(idempotencyKeys[0]); err != nil || parsed.String() != idempotencyKeys[0] {
		return errors.New("registration proof requires a canonical UUID Idempotency-Key")
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, defaultMaxBody+1))
	if err != nil {
		return errors.New("failed to read registration request body")
	}
	if len(body) > defaultMaxBody {
		return errors.New("registration request body exceeds signing limit")
	}
	_ = request.Body.Close()
	request.Body = io.NopCloser(bytes.NewReader(body))
	request.ContentLength = int64(len(body))
	request.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	digest := sha256.Sum256(body)
	request.Header.Set("Content-Digest", "sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":")
	return applySignature(request, engineIdentity, true, registrationTag, now, random)
}

func applySignature(request *http.Request, engineIdentity *identity.Identity, hasBody bool, tag string, now time.Time, random io.Reader) error {
	nonceBytes := make([]byte, 16)
	if _, err := io.ReadFull(random, nonceBytes); err != nil {
		return errors.New("failed to generate signature nonce")
	}
	created := now.Unix()
	expires := created + 300
	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)

	components := `"@method" "@authority" "@path" "@query"`
	if hasBody {
		components += ` "content-digest";sf "content-type" "idempotency-key"`
	}
	params := fmt.Sprintf(`(%s);created=%d;expires=%d;keyid=%q;nonce=%q;tag=%q`, components, created, expires, engineIdentity.AuthenticationKeyID(), nonce, tag)
	signatureBase := buildSignatureBase(request, hasBody, params)
	signature := engineIdentity.Sign([]byte(signatureBase))
	request.Header.Set("Signature-Input", signatureLabel+"="+params)
	request.Header.Set("Signature", signatureLabel+"=:"+base64.StdEncoding.EncodeToString(signature)+":")
	return nil
}

func buildSignatureBase(request *http.Request, hasBody bool, params string) string {
	authority := request.URL.Host
	if request.Host != "" {
		authority = request.Host
	}
	path := request.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	query := "?" + request.URL.RawQuery
	var base strings.Builder
	fmt.Fprintf(&base, "\"@method\": %s\n", strings.ToUpper(request.Method))
	fmt.Fprintf(&base, "\"@authority\": %s\n", authority)
	fmt.Fprintf(&base, "\"@path\": %s\n", path)
	fmt.Fprintf(&base, "\"@query\": %s\n", query)
	if hasBody {
		fmt.Fprintf(&base, "\"content-digest\";sf: %s\n", request.Header.Get("Content-Digest"))
		fmt.Fprintf(&base, "\"content-type\": %s\n", request.Header.Get("Content-Type"))
		fmt.Fprintf(&base, "\"idempotency-key\": %s\n", request.Header.Get("Idempotency-Key"))
	}
	fmt.Fprintf(&base, "\"@signature-params\": %s", params)
	return base.String()
}

// ResolveEngineEndpoint appends a self-only engine route to an HTTP(S) base
// URL. A URL already ending in the requested route is accepted unchanged.
func ResolveEngineEndpoint(baseURL string, route EngineRoute) (*url.URL, error) {
	if !validEngineRoute(route) {
		return nil, errors.New("unsupported engine route")
	}
	endpoint, err := url.Parse(baseURL)
	if err != nil || endpoint.Host == "" || endpoint.Scheme != "http" && endpoint.Scheme != "https" {
		return nil, errors.New("engine endpoint must be an absolute HTTP or HTTPS URL")
	}
	if endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.User != nil {
		return nil, errors.New("engine endpoint base URL must not contain user information, a query, or a fragment")
	}
	cleanPath := strings.TrimSuffix(endpoint.Path, "/")
	if isEngineRoute(cleanPath) {
		if !hasRouteSuffix(cleanPath, route) {
			return nil, errors.New("engine endpoint URL ends in a different engine route")
		}
		endpoint.Path = cleanPath
		endpoint.RawPath = ""
		return endpoint, nil
	}
	return endpoint.JoinPath(strings.TrimPrefix(string(route), "/")), nil
}

func validEngineRoute(route EngineRoute) bool {
	return route == ConfigRoute || route == TelemetryLogRoute || route == TelemetryTracesRoute
}

func isEngineRoute(path string) bool {
	return hasRouteSuffix(path, ConfigRoute) || hasRouteSuffix(path, TelemetryLogRoute) || hasRouteSuffix(path, TelemetryTracesRoute)
}

func hasRouteSuffix(path string, route EngineRoute) bool {
	return path == string(route) || strings.HasSuffix(path, string(route))
}
