package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"

	"github.com/databacker/api/go/api"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/nacl/box"
	"gopkg.in/yaml.v3"

	"github.com/databacker/mysql-backup/pkg/remote"
)

// ProcessConfig reads the configuration from a stream and returns the parsed configuration.
// If the configuration is of type remote, it will retrieve the remote configuration.
// Continues to process remotes until it gets a final valid ConfigSpec or fails.
func ProcessConfig(r io.Reader) (actualConfig *api.ConfigSpec, err error) {
	var (
		conf        api.Config
		credentials []string
	)
	decoder := yaml.NewDecoder(r)
	if err := decoder.Decode(&conf); err != nil {
		return nil, fmt.Errorf("fatal error reading config file: %w", err)
	}

	// check that the version is something we recognize
	if conf.Version != api.ConfigDatabackIoV1 {
		return nil, fmt.Errorf("unknown config version: %s", conf.Version)
	}
	specBytes, err := yaml.Marshal(conf.Spec)
	if err != nil {
		return nil, fmt.Errorf("error marshalling spec part of configuration: %w", err)
	}
	// if the config type is remote, retrieve our remote configuration
	// repeat until we end up with a configuration that is of type local
	for {
		switch conf.Kind {
		case api.Local:
			var spec api.ConfigSpec
			// there is a problem that api.ConfigSpec has json tags but not yaml tags.
			// This is because github.com/databacker/api uses oapi-codegen to generate the api
			// which creates json tags and not yaml tags. There is a PR to get them in.
			// http://github.com/oapi-codegen/oapi-codegen/pull/1798
			// Once that is in, and databacker/api uses them, this will work directly with yaml.
			// For now, because there are no yaml tags, it defaults to just lowercasing the
			// field. That means anything camelcase will be lowercased, which does not always
			// parse properly. For example, `thisField` will expect `thisfield` in the yaml, which
			// is incorrect.
			// We fix this by converting the spec part of the config into json,
			// as yaml is a valid subset of json, and then unmarshalling that.
			if err := yaml.Unmarshal(specBytes, &spec); err != nil {
				return nil, fmt.Errorf("parsed yaml had kind local, but spec invalid")
			}
			actualConfig = &spec
		case api.Remote:
			var spec api.RemoteSpec
			if err := yaml.Unmarshal(specBytes, &spec); err != nil {
				return nil, fmt.Errorf("parsed yaml had kind remote, but spec invalid")
			}
			remoteConfig, err := getRemoteConfig(spec)
			if err != nil {
				return nil, fmt.Errorf("error parsing remote config: %w", err)
			}
			conf = remoteConfig
			// save encryption key for later
			if spec.Credentials != nil {
				credentials = append(credentials, *spec.Credentials)
			}
		case api.Encrypted:
			var spec api.EncryptedSpec
			if err := yaml.Unmarshal(specBytes, &spec); err != nil {
				return nil, fmt.Errorf("parsed yaml had kind encrypted, but spec invalid")
			}
			// now try to decrypt it
			conf, err = decryptConfig(spec, credentials)
			if err != nil {
				return nil, fmt.Errorf("error decrypting config: %w", err)
			}
		default:
			return nil, fmt.Errorf("unknown config type: %s", conf.Kind)
		}
		if actualConfig != nil {
			break
		}
	}
	return actualConfig, nil
}

// getRemoteConfig given a RemoteSpec for a config, retrieve the config from the remote
// and parse it into a Config struct.
func getRemoteConfig(spec api.RemoteSpec) (conf api.Config, err error) {
	if spec.URL == nil || spec.Certificates == nil || spec.Credentials == nil {
		return conf, errors.New("empty fields for components")
	}
	resp, err := remote.OpenConnection(*spec.URL, *spec.Certificates, *spec.Credentials)
	if err != nil {
		return conf, fmt.Errorf("error getting reader: %w", err)
	}
	defer resp.Body.Close()

	// Read the body of the response and convert to a config.Config struct
	var baseConf api.Config
	decoder := yaml.NewDecoder(resp.Body)
	if err := decoder.Decode(&baseConf); err != nil {
		return conf, fmt.Errorf("invalid config file retrieved from server: %w", err)
	}

	return baseConf, nil
}

// decryptConfig decrypt an EncryptedSpec given an EncryptedSpec and a list of credentials.
// Returns the decrypted Config struct.
func decryptConfig(spec api.EncryptedSpec, credentials []string) (api.Config, error) {
	var plainConfig api.Config
	if spec.Algorithm == nil {
		return plainConfig, errors.New("empty algorithm")
	}
	if spec.RecipientPublicKey == nil {
		return plainConfig, errors.New("empty recipient public key")
	}
	if spec.SenderPublicKey == nil {
		return plainConfig, errors.New("empty sender public key")
	}
	if spec.Data == nil {
		return plainConfig, errors.New("empty data")
	}
	// make sure we have the key matching the public key
	var (
		privateKey *ecdh.PrivateKey
		curve      = ecdh.X25519()
	)

	for _, cred := range credentials {
		// get our curve25519 private key
		keyBytes, err := base64.StdEncoding.DecodeString(cred)
		if err != nil {
			return plainConfig, fmt.Errorf("error decoding credentials: %w", err)
		}
		if len(keyBytes) != ed25519.SeedSize {
			return plainConfig, fmt.Errorf("invalid key size %d, must be %d", len(keyBytes), ed25519.SeedSize)
		}
		candidatePrivateKey, err := curve.NewPrivateKey(keyBytes)
		if err != nil {
			return plainConfig, fmt.Errorf("error creating private key: %w", err)
		}
		// get the public key from the private key
		candidatePublicKey := candidatePrivateKey.PublicKey()
		// check if the public key matches the one we have, if so, break
		pubKeyBase64 := base64.StdEncoding.EncodeToString(candidatePublicKey.Bytes())
		if pubKeyBase64 == *spec.RecipientPublicKey {
			privateKey = candidatePrivateKey
			break
		}
	}
	// if we didn't find a matching key, return an error
	if privateKey == nil {
		return plainConfig, fmt.Errorf("no private key found that matches public key %s", *spec.RecipientPublicKey)
	}
	senderPublicKeyBytes, err := base64.StdEncoding.DecodeString(*spec.SenderPublicKey)
	if err != nil {
		return plainConfig, fmt.Errorf("failed to decode sender public key: %w", err)
	}

	// Derive the shared secret using the sender's public key and receiver's private key
	var senderPublicKey, receiverPrivateKey, sharedSecret [32]byte
	copy(senderPublicKey[:], senderPublicKeyBytes)
	copy(receiverPrivateKey[:], privateKey.Bytes()) // Use the seed to get the private scalar
	box.Precompute(&sharedSecret, &senderPublicKey, &receiverPrivateKey)

	// Derive a symmetric key using HKDF with the shared secret
	hkdfReader := hkdf.New(sha256.New, sharedSecret[:], nil, []byte(api.SymmetricKey))
	var symmetricKeySize int
	switch *spec.Algorithm {
	case api.AesGcm256:
		symmetricKeySize = 32
	case api.Chacha20Poly1305:
		symmetricKeySize = 32
	default:
		return plainConfig, fmt.Errorf("unsupported algorithm: %s", *spec.Algorithm)
	}
	symmetricKey := make([]byte, symmetricKeySize)
	if _, err := hkdfReader.Read(symmetricKey); err != nil {
		return plainConfig, fmt.Errorf("failed to derive symmetric key: %w", err)
	}

	var (
		plaintext []byte
		aead      cipher.AEAD
	)
	encryptedData, err := base64.StdEncoding.DecodeString(*spec.Data)
	if err != nil {
		return plainConfig, fmt.Errorf("failed to decode encrypted data: %w", err)
	}
	switch *spec.Algorithm {
	case api.AesGcm256:
		// Decrypt with AES-GCM
		block, err := aes.NewCipher(symmetricKey)
		if err != nil {
			return plainConfig, fmt.Errorf("failed to initialize AES cipher: %w", err)
		}
		aead, err = cipher.NewGCM(block)
		if err != nil {
			return plainConfig, fmt.Errorf("failed to initialize AES-GCM: %w", err)
		}
	case api.Chacha20Poly1305:
		// Decrypt with ChaCha20Poly1305
		aead, err = chacha20poly1305.New(symmetricKey)
		if err != nil {
			return plainConfig, fmt.Errorf("failed to initialize ChaCha20Poly1305: %w", err)
		}
	default:
		return plainConfig, fmt.Errorf("unsupported algorithm: %s", *spec.Algorithm)
	}
	if len(encryptedData) < aead.NonceSize() {
		return plainConfig, errors.New("invalid encrypted data length")
	}
	dataNonce := encryptedData[:aead.NonceSize()]
	ciphertext := encryptedData[aead.NonceSize():]
	plaintext, err = aead.Open(nil, dataNonce, ciphertext, nil)
	if err != nil {
		return plainConfig, fmt.Errorf("failed to decrypt data: %w", err)
	}
	if err := yaml.Unmarshal(plaintext, &plainConfig); err != nil {
		return plainConfig, fmt.Errorf("parsed yaml had kind remote, but spec invalid")
	}
	return plainConfig, nil
}
