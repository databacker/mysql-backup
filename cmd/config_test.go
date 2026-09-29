package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/databacker/api/go/api"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/databacker/mysql-backup/pkg/identity"
)

func configTestCredentials() api.EngineCredentials {
	return api.EngineCredentials{
		Version:                           api.DatabackerCredentialsV2,
		Seed:                              "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=",
		AuthenticationGeneration:          1,
		ConfigurationEncryptionGeneration: 1,
	}
}

func writeConfigTestCredentials(t *testing.T, credentials api.EngineCredentials) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "credentials.yaml")
	encoded, err := yaml.Marshal(credentials)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestConfigPublicInfo(t *testing.T) {
	credentialsFile := writeConfigTestCredentials(t, configTestCredentials())
	command := publicInfoCmd()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--credentials-file", credentialsFile})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	var info publicInfo
	if err := json.Unmarshal(output.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.Authentication.KeyID != "auth:10ae5b498547bbbb760358b0f2ee659ccbf17e8858263eff0c06225c6ebbaf3e" {
		t.Fatalf("authentication key ID = %q", info.Authentication.KeyID)
	}
	if info.ConfigurationEncryption.KeyID != "config:5be9cbe48dcffda73ffc1befe2993e080f19c3af4cececba34016079e948c45a" {
		t.Fatalf("configuration key ID = %q", info.ConfigurationEncryption.KeyID)
	}
}

func TestRootConfigCommandSkipsRuntimeSetup(t *testing.T) {
	credentialsFile := writeConfigTestCredentials(t, configTestCredentials())
	root, err := rootCmd(nil)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"config", "public-info", "--credentials-file", credentialsFile})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"authentication"`) {
		t.Fatalf("unexpected output: %s", output.String())
	}
}

func TestConfigRegistrationBundle(t *testing.T) {
	credentialsFile := writeConfigTestCredentials(t, configTestCredentials())
	command := registrationBundleCmd()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{
		"--credentials-file", credentialsFile,
		"--url", "https://cloud.example/admin/accounts/account-123/engines",
		"--name", "fixture-engine",
		"--description", "Fixture registration",
		"--idempotency-key", "550e8400-e29b-41d4-a716-446655440000",
	})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	var bundle signedRequestBundle
	if err := json.Unmarshal(output.Bytes(), &bundle); err != nil {
		t.Fatal(err)
	}
	wantBody := `{"name":"fixture-engine","description":"Fixture registration","publicKeys":{"authentication":{"algorithm":"ed25519","generation":1,"publicKey":"bCfocojhXze28IS3hlHnttC+OO469HweOvPruf3q8wY="},"configurationEncryption":{"algorithm":"x25519","generation":1,"publicKey":"XwG2fwzzSPppHov/SmmQ2SlsfbgAqscGd+UztjrLOEo="}}}`
	if bundle.Body != wantBody {
		t.Fatalf("body = %q, want %q", bundle.Body, wantBody)
	}
	if bundle.Method != "POST" || bundle.ContentDigest != "sha-256=:Dbgi/FOEu12LsF3Mu/XvGHSSIgqrx/lxp8i0QpuFF6Y=:" {
		t.Fatalf("unexpected registration bundle: %#v", bundle)
	}
	if !strings.Contains(bundle.SignatureInput, `tag="databacker-engine-registration-v1"`) || bundle.Signature == "" {
		t.Fatalf("registration proof is incomplete: %#v", bundle)
	}
}

func TestConfigRotations(t *testing.T) {
	tests := []struct {
		name  string
		cmd   func() *cobra.Command
		check func(*testing.T, api.EngineCredentials)
	}{
		{
			name: "authentication",
			cmd:  rotateAuthenticationCmd,
			check: func(t *testing.T, credentials api.EngineCredentials) {
				if credentials.AuthenticationGeneration != 2 || credentials.ConfigurationEncryptionGeneration != 1 {
					t.Fatalf("unexpected generations: %#v", credentials)
				}
			},
		},
		{
			name: "configuration",
			cmd:  rotateConfigurationKeyCmd,
			check: func(t *testing.T, credentials api.EngineCredentials) {
				if credentials.AuthenticationGeneration != 1 || credentials.ConfigurationEncryptionGeneration != 2 {
					t.Fatalf("unexpected generations: %#v", credentials)
				}
				if credentials.RetainedConfigurationEncryptionGenerations == nil || len(*credentials.RetainedConfigurationEncryptionGenerations) != 1 || (*credentials.RetainedConfigurationEncryptionGenerations)[0] != 1 {
					t.Fatalf("previous generation was not retained: %#v", credentials)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			credentialsFile := writeConfigTestCredentials(t, configTestCredentials())
			outputFile := filepath.Join(t.TempDir(), "rotated.yaml")
			command := test.cmd()
			var output bytes.Buffer
			command.SetOut(&output)
			command.SetErr(io.Discard)
			command.SetArgs([]string{
				"--credentials-file", credentialsFile,
				"--output", outputFile,
				"--url", "https://cloud.example/admin/accounts/account-123/engines/engine-123",
				"--name", "fixture-engine",
			})
			if err := command.Execute(); err != nil {
				t.Fatal(err)
			}
			rotated, err := readCredentials(outputFile)
			if err != nil {
				t.Fatal(err)
			}
			test.check(t, rotated)
			var bundle signedRequestBundle
			if err := json.Unmarshal(output.Bytes(), &bundle); err != nil {
				t.Fatal(err)
			}
			if bundle.Method != "PATCH" || bundle.Signature == "" {
				t.Fatalf("unexpected update bundle: %#v", bundle)
			}
		})
	}
}

func TestConfigCredentialsGenerateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.yaml")
	command := generateCredentialsCmd()
	command.SetArgs([]string{"--output", path})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	credentials, err := readCredentials(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.New(credentials); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("credentials permissions = %o", info.Mode().Perm())
		}
	}
	if err := command.Execute(); err == nil {
		t.Fatal("expected existing output file to be rejected")
	}
}

func TestConfigRotationRefusesActiveCredentialsPath(t *testing.T) {
	credentialsFile := writeConfigTestCredentials(t, configTestCredentials())
	command := rotateAuthenticationCmd()
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	command.SetArgs([]string{
		"--credentials-file", credentialsFile,
		"--output", credentialsFile,
		"--force",
		"--url", "https://cloud.example/admin/accounts/account-123/engines/engine-123",
		"--name", "fixture-engine",
	})
	if err := command.Execute(); err == nil {
		t.Fatal("expected in-place credential rotation to fail")
	}
	credentials, err := readCredentials(credentialsFile)
	if err != nil {
		t.Fatal(err)
	}
	if credentials.AuthenticationGeneration != 1 {
		t.Fatal("active credentials were modified")
	}
}
