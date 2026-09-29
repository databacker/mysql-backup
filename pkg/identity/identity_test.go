package identity

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/databacker/api/go/api"
)

func fixtureCredentials() api.EngineCredentials {
	return api.EngineCredentials{
		Version:                           api.DatabackerCredentialsV2,
		Seed:                              "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=",
		AuthenticationGeneration:          1,
		ConfigurationEncryptionGeneration: 1,
	}
}

func TestFixtureDerivation(t *testing.T) {
	identity, err := New(fixtureCredentials())
	if err != nil {
		t.Fatal(err)
	}
	if got := base64.StdEncoding.EncodeToString(identity.AuthenticationPublicKey()); got != "bCfocojhXze28IS3hlHnttC+OO469HweOvPruf3q8wY=" {
		t.Fatalf("authentication public key = %q", got)
	}
	if got := identity.AuthenticationKeyID(); got != "auth:10ae5b498547bbbb760358b0f2ee659ccbf17e8858263eff0c06225c6ebbaf3e" {
		t.Fatalf("authentication key ID = %q", got)
	}
	privateKey, err := identity.ConfigurationPrivateKey(1)
	if err != nil {
		t.Fatal(err)
	}
	if got := base64.StdEncoding.EncodeToString(privateKey.PublicKey().Bytes()); got != "XwG2fwzzSPppHov/SmmQ2SlsfbgAqscGd+UztjrLOEo=" {
		t.Fatalf("configuration public key = %q", got)
	}
	publicKey, err := identity.ConfigurationPublicKey(1)
	if err != nil {
		t.Fatal(err)
	}
	if got := base64.StdEncoding.EncodeToString(publicKey); got != "XwG2fwzzSPppHov/SmmQ2SlsfbgAqscGd+UztjrLOEo=" {
		t.Fatalf("configuration public key method = %q", got)
	}
	if got, err := identity.ConfigurationKeyID(1); err != nil || got != "config:5be9cbe48dcffda73ffc1befe2993e080f19c3af4cececba34016079e948c45a" {
		t.Fatalf("configuration key ID = %q, err=%v", got, err)
	}
}

func TestCredentialValidation(t *testing.T) {
	retainedDuplicate := []uint64{2, 2}
	retainedActive := []uint64{1}
	tests := []struct {
		name   string
		modify func(*api.EngineCredentials)
	}{
		{"version", func(c *api.EngineCredentials) { c.Version = "old" }},
		{"base64", func(c *api.EngineCredentials) { c.Seed = "not-base64" }},
		{"non-canonical base64", func(c *api.EngineCredentials) { c.Seed += "\n" }},
		{"length", func(c *api.EngineCredentials) { c.Seed = base64.StdEncoding.EncodeToString(make([]byte, 31)) }},
		{"authentication generation", func(c *api.EngineCredentials) { c.AuthenticationGeneration = 0 }},
		{"configuration generation", func(c *api.EngineCredentials) { c.ConfigurationEncryptionGeneration = 0 }},
		{"duplicate retained", func(c *api.EngineCredentials) { c.RetainedConfigurationEncryptionGenerations = &retainedDuplicate }},
		{"active retained", func(c *api.EngineCredentials) { c.RetainedConfigurationEncryptionGenerations = &retainedActive }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			credentials := fixtureCredentials()
			test.modify(&credentials)
			if _, err := New(credentials); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestIdentityDoesNotFormatSecret(t *testing.T) {
	identity, err := New(fixtureCredentials())
	if err != nil {
		t.Fatal(err)
	}
	formatted := fmt.Sprintf("%v %#v", identity, identity)
	if strings.Contains(formatted, fixtureCredentials().Seed) || strings.Contains(formatted, "[0 1 2 3 4 5") {
		t.Fatal("identity formatting exposed its seed")
	}
}
