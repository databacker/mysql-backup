// Package identity derives and uses the purpose-specific engine keys.
package identity

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/databacker/api/go/api"
	"golang.org/x/crypto/hkdf"
)

const (
	seedSize               = 32
	maxRetainedGenerations = 16
	credentialSalt         = "databacker engine credential v2"
	authenticationInfo     = "databacker/authentication/ed25519/v1/generation/"
	configurationInfo      = "databacker/configuration-encryption/x25519/v1/generation/"
	authenticationIDDomain = "databacker/engine-key-id/authentication/ed25519/v1\x00"
	configurationIDDomain  = "databacker/engine-key-id/configuration-encryption/x25519/v1\x00"
)

// Identity contains validated credential state. Its secret material is
// intentionally not printable or serializable.
type Identity struct {
	seed                  [seedSize]byte
	authenticationGen     uint64
	configurationGen      uint64
	retainedConfiguration []uint64
}

// String deliberately exposes only non-secret generation metadata.
func (i *Identity) String() string {
	return fmt.Sprintf("engine identity (authentication generation %d, configuration generation %d)", i.authenticationGen, i.configurationGen)
}

// GoString prevents %#v diagnostics from exposing the seed.
func (i *Identity) GoString() string { return i.String() }

// New validates a versioned engine credential.
func New(credentials api.EngineCredentials) (*Identity, error) {
	if credentials.Version != api.DatabackerCredentialsV2 {
		return nil, fmt.Errorf("unsupported engine credential version")
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(credentials.Seed)
	if err != nil || base64.StdEncoding.EncodeToString(decoded) != credentials.Seed {
		return nil, fmt.Errorf("invalid engine credential seed encoding")
	}
	if len(decoded) != seedSize {
		return nil, fmt.Errorf("invalid engine credential seed length")
	}
	if credentials.AuthenticationGeneration == 0 || credentials.ConfigurationEncryptionGeneration == 0 {
		return nil, errors.New("engine credential generations must be positive")
	}

	identity := &Identity{
		authenticationGen: credentials.AuthenticationGeneration,
		configurationGen:  credentials.ConfigurationEncryptionGeneration,
	}
	copy(identity.seed[:], decoded)

	if credentials.RetainedConfigurationEncryptionGenerations != nil {
		retained := *credentials.RetainedConfigurationEncryptionGenerations
		if len(retained) > maxRetainedGenerations {
			return nil, errors.New("too many retained configuration generations")
		}
		seen := make(map[uint64]struct{}, len(retained))
		for _, generation := range retained {
			if generation == 0 || generation == identity.configurationGen {
				return nil, errors.New("invalid retained configuration generation")
			}
			if _, ok := seen[generation]; ok {
				return nil, errors.New("duplicate retained configuration generation")
			}
			seen[generation] = struct{}{}
			identity.retainedConfiguration = append(identity.retainedConfiguration, generation)
		}
	}

	return identity, nil
}

// AuthenticationGeneration returns the active authentication generation.
func (i *Identity) AuthenticationGeneration() uint64 { return i.authenticationGen }

// ConfigurationGeneration returns the active configuration generation.
func (i *Identity) ConfigurationGeneration() uint64 { return i.configurationGen }

// Sign signs a protocol message with the active Ed25519 authentication key.
func (i *Identity) Sign(message []byte) []byte {
	return ed25519.Sign(i.authenticationPrivateKey(), message)
}

// AuthenticationPublicKey returns a copy of the active raw Ed25519 public key.
func (i *Identity) AuthenticationPublicKey() []byte {
	publicKey := i.authenticationPrivateKey().Public().(ed25519.PublicKey)
	return append([]byte(nil), publicKey...)
}

// AuthenticationKeyID returns the deterministic active authentication fingerprint.
func (i *Identity) AuthenticationKeyID() string {
	return keyID("auth", authenticationIDDomain, i.authenticationGen, i.AuthenticationPublicKey())
}

// ConfigurationPrivateKey returns the requested active or retained X25519 key.
func (i *Identity) ConfigurationPrivateKey(generation uint64) (*ecdh.PrivateKey, error) {
	if !i.hasConfigurationGeneration(generation) {
		return nil, errors.New("configuration generation is not active or retained")
	}
	input, err := i.derive(configurationInfo, generation)
	if err != nil {
		return nil, errors.New("failed to derive configuration key")
	}
	key, err := ecdh.X25519().NewPrivateKey(input)
	if err != nil {
		return nil, errors.New("failed to construct configuration key")
	}
	return key, nil
}

// ConfigurationKeyID returns the deterministic fingerprint for an active or retained key.
func (i *Identity) ConfigurationKeyID(generation uint64) (string, error) {
	key, err := i.ConfigurationPrivateKey(generation)
	if err != nil {
		return "", err
	}
	return keyID("config", configurationIDDomain, generation, key.PublicKey().Bytes()), nil
}

// ConfigurationPublicKey returns a copy of the requested active or retained
// raw X25519 public key.
func (i *Identity) ConfigurationPublicKey(generation uint64) ([]byte, error) {
	key, err := i.ConfigurationPrivateKey(generation)
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), key.PublicKey().Bytes()...), nil
}

func (i *Identity) authenticationPrivateKey() ed25519.PrivateKey {
	seed, err := i.derive(authenticationInfo, i.authenticationGen)
	if err != nil {
		panic("identity: fixed-length HKDF derivation failed")
	}
	return ed25519.NewKeyFromSeed(seed)
}

func (i *Identity) derive(info string, generation uint64) ([]byte, error) {
	prk := hkdf.Extract(sha256.New, i.seed[:], []byte(credentialSalt))
	var encodedGeneration [8]byte
	binary.BigEndian.PutUint64(encodedGeneration[:], generation)
	reader := hkdf.Expand(sha256.New, prk, append([]byte(info), encodedGeneration[:]...))
	derived := make([]byte, 32)
	if _, err := io.ReadFull(reader, derived); err != nil {
		return nil, err
	}
	return derived, nil
}

func (i *Identity) hasConfigurationGeneration(generation uint64) bool {
	if generation == i.configurationGen {
		return true
	}
	for _, retained := range i.retainedConfiguration {
		if generation == retained {
			return true
		}
	}
	return false
}

func keyID(prefix, domain string, generation uint64, publicKey []byte) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(domain))
	var encodedGeneration [8]byte
	binary.BigEndian.PutUint64(encodedGeneration[:], generation)
	_, _ = hash.Write(encodedGeneration[:])
	_, _ = hash.Write(publicKey)
	return prefix + ":" + hex.EncodeToString(hash.Sum(nil))
}
