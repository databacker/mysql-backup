package config

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/databacker/api/go/api"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
	"gopkg.in/yaml.v3"

	"github.com/databacker/mysql-backup/pkg/identity"
	"github.com/databacker/mysql-backup/pkg/remote"
)

const (
	maxConfigBytes = 4 << 20
	maxChainDepth  = 8
	fetchTimeout   = 30 * time.Second
)

// Result contains the final operational configuration and the validated
// identity state used to retrieve it. Rollback state is intentionally omitted
// until rollback protection is implemented.
type Result struct {
	Config   *api.ConfigSpec
	Identity *identity.Identity
}

// ClientFactory allows tests to inject an HTTP client. Production callers use
// the signed HTTP client constructed by the loader.
type ClientFactory func(api.RemoteSpec, *identity.Identity) (*http.Client, error)

// Loader traverses composable Config documents.
type Loader struct {
	ClientFactory ClientFactory
}

// ProcessConfig reads and resolves a local configuration document.
func ProcessConfig(reader io.Reader) (*api.ConfigSpec, error) {
	result, err := (Loader{}).Process(reader)
	if err != nil {
		return nil, err
	}
	return result.Config, nil
}

// Process resolves local, remote, and encrypted documents until it reaches a
// local ConfigSpec.
func (loader Loader) Process(reader io.Reader) (*Result, error) {
	document, err := io.ReadAll(io.LimitReader(reader, maxConfigBytes+1))
	if err != nil {
		return nil, fmt.Errorf("fatal error reading config file: %w", err)
	}
	if len(document) > maxConfigBytes {
		return nil, errors.New("config file exceeds size limit")
	}
	var current api.Config
	decoder := yaml.NewDecoder(bytes.NewReader(document))
	if err := decoder.Decode(&current); err != nil {
		return nil, fmt.Errorf("fatal error reading config file: %w", err)
	}

	result := &Result{}
	seenURLs := make(map[string]struct{})
	for depth := 0; depth < maxChainDepth; depth++ {
		if current.Version != api.ConfigDatabackIoV1 {
			return nil, fmt.Errorf("unknown config version: %s", current.Version)
		}
		switch current.Kind {
		case api.Local:
			var spec api.ConfigSpec
			if err := decodeSpec(current.Spec, &spec); err != nil {
				return nil, errors.New("config kind local has an invalid spec")
			}
			result.Config = &spec
			return result, nil

		case api.Remote:
			var spec api.RemoteSpec
			if err := decodeSpec(current.Spec, &spec); err != nil {
				return nil, errors.New("config kind remote has an invalid spec")
			}
			endpoint, err := remote.ResolveEngineEndpoint(spec.URL, remote.ConfigRoute)
			if err != nil {
				return nil, fmt.Errorf("invalid remote configuration URL: %w", err)
			}
			if _, exists := seenURLs[endpoint.String()]; exists {
				return nil, errors.New("remote configuration cycle detected")
			}
			seenURLs[endpoint.String()] = struct{}{}

			engineIdentity, err := identity.New(spec.Credentials)
			if err != nil {
				return nil, fmt.Errorf("invalid remote engine credentials: %w", err)
			}
			clientFactory := loader.ClientFactory
			if clientFactory == nil {
				clientFactory = signedClient
			}
			client, err := clientFactory(spec, engineIdentity)
			if err != nil {
				return nil, fmt.Errorf("unable to construct remote client: %w", err)
			}
			current, err = fetchRemote(client, endpoint.String())
			if err != nil {
				return nil, fmt.Errorf("unable to retrieve remote config: %w", err)
			}
			result.Identity = engineIdentity

		case api.Encrypted:
			if result.Identity == nil {
				return nil, errors.New("encrypted configuration has no local engine identity")
			}
			var spec api.EncryptedSpec
			if err := decodeSpec(current.Spec, &spec); err != nil {
				return nil, errors.New("config kind encrypted has an invalid spec")
			}
			decrypted, err := decryptConfig(spec, result.Identity)
			if err != nil {
				return nil, fmt.Errorf("unable to decrypt config: %w", err)
			}
			current = decrypted

		default:
			return nil, fmt.Errorf("unknown config type: %s", current.Kind)
		}
	}
	return nil, errors.New("configuration chain exceeds maximum depth")
}

func signedClient(spec api.RemoteSpec, engineIdentity *identity.Identity) (*http.Client, error) {
	pins := []string(nil)
	if spec.Certificates != nil {
		pins = append(pins, (*spec.Certificates)...)
	}
	return remote.NewSignedClient(pins, engineIdentity)
}

func fetchRemote(client *http.Client, endpoint string) (api.Config, error) {
	var config api.Config
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return config, errors.New("invalid remote request")
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return config, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return config, fmt.Errorf("remote returned HTTP status %d", response.StatusCode)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return config, errors.New("remote returned an unsupported content type")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxConfigBytes+1))
	if err != nil {
		return config, errors.New("failed to read remote config")
	}
	if len(body) > maxConfigBytes {
		return config, errors.New("remote config exceeds size limit")
	}
	if err := decodeJSONDocument(body, &config); err != nil {
		return config, errors.New("remote returned an invalid config document")
	}
	return config, nil
}

func decodeSpec(spec map[string]interface{}, target any) error {
	encoded, err := yaml.Marshal(spec)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(encoded, target)
}

func decodeJSONDocument(document []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(document))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("document contains trailing data")
	}
	return nil
}

func decryptConfig(spec api.EncryptedSpec, engineIdentity *identity.Identity) (api.Config, error) {
	var config api.Config
	if spec.Version != api.DatabackerEncryptedConfigV2 ||
		spec.KeyAgreement != api.X25519 ||
		spec.KeyDerivation != api.HkdfSha256 ||
		spec.Encryption != api.EncryptedSpecEncryptionChacha20Poly1305 ||
		spec.PlaintextMediaType != api.ApplicationJSON {
		return config, errors.New("unsupported encrypted configuration profile")
	}
	if spec.ConfigurationVersion == 0 || spec.RecipientGeneration == 0 {
		return config, errors.New("invalid encrypted configuration version")
	}
	privateKey, err := engineIdentity.ConfigurationPrivateKey(spec.RecipientGeneration)
	if err != nil {
		return config, errors.New("encrypted configuration targets an unavailable key generation")
	}
	wantedKeyID, err := engineIdentity.ConfigurationKeyID(spec.RecipientGeneration)
	if err != nil || wantedKeyID != spec.RecipientKeyID {
		return config, errors.New("encrypted configuration targets a different recipient key")
	}
	senderPublicBytes, err := decodeStrictBase64(spec.SenderPublicKey, 32)
	if err != nil {
		return config, errors.New("invalid encrypted configuration sender key")
	}
	nonce, err := decodeStrictBase64(spec.Nonce, chacha20poly1305.NonceSize)
	if err != nil {
		return config, errors.New("invalid encrypted configuration nonce")
	}
	ciphertext, err := base64.StdEncoding.Strict().DecodeString(spec.Ciphertext)
	if err != nil || base64.StdEncoding.EncodeToString(ciphertext) != spec.Ciphertext || len(ciphertext) < chacha20poly1305.Overhead || len(ciphertext) > maxConfigBytes+chacha20poly1305.Overhead {
		return config, errors.New("invalid encrypted configuration ciphertext")
	}
	senderPublicKey, err := ecdh.X25519().NewPublicKey(senderPublicBytes)
	if err != nil {
		return config, errors.New("invalid encrypted configuration sender key")
	}
	sharedSecret, err := privateKey.ECDH(senderPublicKey)
	if err != nil {
		return config, errors.New("encrypted configuration key agreement failed")
	}
	aad := envelopeAAD(spec, senderPublicBytes, nonce)
	prk := hkdf.Extract(sha256.New, sharedSecret, []byte("databacker configuration encryption v2"))
	keyReader := hkdf.Expand(sha256.New, prk, append([]byte("databacker/configuration-aead-key/chacha20-poly1305/v2\x00"), aad...))
	aeadKey := make([]byte, chacha20poly1305.KeySize)
	if _, err := io.ReadFull(keyReader, aeadKey); err != nil {
		return config, errors.New("encrypted configuration key derivation failed")
	}
	aead, err := chacha20poly1305.New(aeadKey)
	if err != nil {
		return config, errors.New("encrypted configuration cipher initialization failed")
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return config, errors.New("encrypted configuration authentication failed")
	}
	if len(plaintext) > maxConfigBytes || decodeJSONDocument(plaintext, &config) != nil {
		return api.Config{}, errors.New("encrypted configuration plaintext is invalid")
	}
	return config, nil
}

func decodeStrictBase64(encoded string, length int) ([]byte, error) {
	decoded, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(decoded) != length || base64.StdEncoding.EncodeToString(decoded) != encoded {
		return nil, errors.New("invalid base64 value")
	}
	return decoded, nil
}

func envelopeAAD(spec api.EncryptedSpec, senderPublicKey, nonce []byte) []byte {
	var aad bytes.Buffer
	writeOpaque(&aad, []byte("databacker/configuration-envelope-aad/v2"))
	writeOpaque(&aad, []byte(spec.Version))
	_ = binary.Write(&aad, binary.BigEndian, spec.ConfigurationVersion)
	writeOpaque(&aad, []byte(spec.RecipientKeyID))
	_ = binary.Write(&aad, binary.BigEndian, spec.RecipientGeneration)
	writeOpaque(&aad, []byte(spec.KeyAgreement))
	writeOpaque(&aad, []byte(spec.KeyDerivation))
	writeOpaque(&aad, []byte(spec.Encryption))
	writeOpaque(&aad, []byte(spec.PlaintextMediaType))
	writeOpaque(&aad, senderPublicKey)
	writeOpaque(&aad, nonce)
	return aad.Bytes()
}

func writeOpaque(writer io.Writer, value []byte) {
	_ = binary.Write(writer, binary.BigEndian, uint32(len(value)))
	_, _ = writer.Write(value)
}
