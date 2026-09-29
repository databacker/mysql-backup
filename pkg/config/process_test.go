package config

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/databacker/api/go/api"

	"github.com/databacker/mysql-backup/pkg/identity"
)

func testCredentials() api.EngineCredentials {
	return api.EngineCredentials{Version: api.DatabackerCredentialsV2, Seed: "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=", AuthenticationGeneration: 1, ConfigurationEncryptionGeneration: 1}
}

func TestProcessLocalConfig(t *testing.T) {
	document := "version: config.databack.io/v1\nkind: local\nmetadata: {}\nspec:\n  logging: info\n"
	result, err := ProcessConfig(strings.NewReader(document))
	if err != nil {
		t.Fatal(err)
	}
	if result.Logging == nil || *result.Logging != api.Info {
		t.Fatalf("unexpected local config: %#v", result)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestProcessRemoteReevaluatesRetrievedSpec(t *testing.T) {
	remoteDocument := api.Config{Version: api.ConfigDatabackIoV1, Kind: api.Local, Metadata: api.Metadata{}, Spec: map[string]any{"logging": "debug"}}
	encoded, _ := json.Marshal(remoteDocument)
	loader := Loader{ClientFactory: func(_ api.RemoteSpec, _ *identity.Identity) (*http.Client, error) {
		return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.String() != "https://cloud.example/engines/config" {
				t.Fatalf("request URL = %q", request.URL)
			}
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(encoded))}, nil
		})}, nil
	}}
	bootstrap := "version: config.databack.io/v1\nkind: remote\nmetadata: {}\nspec:\n  url: https://cloud.example\n  credentials:\n    version: databacker-credentials/v2\n    seed: AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=\n    authenticationGeneration: 1\n    configurationEncryptionGeneration: 1\n"
	result, err := loader.Process(strings.NewReader(bootstrap))
	if err != nil {
		t.Fatal(err)
	}
	if result.Config.Logging == nil || *result.Config.Logging != api.Debug {
		t.Fatalf("retrieved config was not processed: %#v", result.Config)
	}
}

func TestDecryptSharedFixture(t *testing.T) {
	engineIdentity, err := identity.New(testCredentials())
	if err != nil {
		t.Fatal(err)
	}
	spec := api.EncryptedSpec{
		Version: api.DatabackerEncryptedConfigV2, ConfigurationVersion: 1,
		RecipientKeyID: "config:5be9cbe48dcffda73ffc1befe2993e080f19c3af4cececba34016079e948c45a", RecipientGeneration: 1,
		KeyAgreement: api.X25519, KeyDerivation: api.HkdfSha256, Encryption: api.EncryptedSpecEncryptionChacha20Poly1305,
		SenderPublicKey: "NYBy1jZYgNGu6jKa35EhODhR7SGijjt16WXQ0s0WYlQ=", Nonce: "AAECAwQFBgcICQoL",
		Ciphertext:         "UZGvDJSg+C84SF9gwPdNKxC2oUhx1+0O44gy8hgNKnIlTRu5kTJWmRVUDywXkZp+meEdHNOfhNXamwZYJgR9oDMVrR2N2Qx1SAJCXTiwvO8nzxQ2SUA2tKAPfDUJXvfB1PAfNHG8gyAj4LC03uHivCpJxo3qCFztgSw=",
		PlaintextMediaType: api.ApplicationJSON,
	}
	decrypted, err := decryptConfig(spec, engineIdentity)
	if err != nil {
		t.Fatal(err)
	}
	if decrypted.Kind != api.Local || decrypted.Spec["logging"] != "info" {
		t.Fatalf("unexpected plaintext: %#v", decrypted)
	}
	spec.Ciphertext = spec.Ciphertext[:len(spec.Ciphertext)-2] + "0="
	if _, err := decryptConfig(spec, engineIdentity); err == nil {
		t.Fatal("expected tampered ciphertext to fail")
	}
}

func TestDecryptSharedFixtureWithRetainedGeneration(t *testing.T) {
	credentials := testCredentials()
	credentials.ConfigurationEncryptionGeneration = 2
	retained := []uint64{1}
	credentials.RetainedConfigurationEncryptionGenerations = &retained
	engineIdentity, err := identity.New(credentials)
	if err != nil {
		t.Fatal(err)
	}
	spec := api.EncryptedSpec{
		Version: api.DatabackerEncryptedConfigV2, ConfigurationVersion: 1,
		RecipientKeyID: "config:5be9cbe48dcffda73ffc1befe2993e080f19c3af4cececba34016079e948c45a", RecipientGeneration: 1,
		KeyAgreement: api.X25519, KeyDerivation: api.HkdfSha256, Encryption: api.EncryptedSpecEncryptionChacha20Poly1305,
		SenderPublicKey: "NYBy1jZYgNGu6jKa35EhODhR7SGijjt16WXQ0s0WYlQ=", Nonce: "AAECAwQFBgcICQoL",
		Ciphertext:         "UZGvDJSg+C84SF9gwPdNKxC2oUhx1+0O44gy8hgNKnIlTRu5kTJWmRVUDywXkZp+meEdHNOfhNXamwZYJgR9oDMVrR2N2Qx1SAJCXTiwvO8nzxQ2SUA2tKAPfDUJXvfB1PAfNHG8gyAj4LC03uHivCpJxo3qCFztgSw=",
		PlaintextMediaType: api.ApplicationJSON,
	}
	if _, err := decryptConfig(spec, engineIdentity); err != nil {
		t.Fatalf("retained generation did not decrypt: %v", err)
	}
	spec.ConfigurationVersion++
	if _, err := decryptConfig(spec, engineIdentity); err == nil {
		t.Fatal("expected authenticated metadata change to fail")
	}
}
