package cmd

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/databacker/api/go/api"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/databacker/mysql-backup/pkg/identity"
	"github.com/databacker/mysql-backup/pkg/remote"
)

type publicKeyInfo struct {
	Algorithm  string `json:"algorithm"`
	Generation uint64 `json:"generation"`
	PublicKey  string `json:"publicKey"`
	KeyID      string `json:"keyId"`
}

type publicInfo struct {
	Authentication          publicKeyInfo `json:"authentication"`
	ConfigurationEncryption publicKeyInfo `json:"configurationEncryption"`
}

type registrationPublicKey struct {
	Algorithm  string `json:"algorithm"`
	Generation uint64 `json:"generation"`
	PublicKey  string `json:"publicKey"`
}

type registrationPublicKeys struct {
	Authentication          registrationPublicKey `json:"authentication"`
	ConfigurationEncryption registrationPublicKey `json:"configurationEncryption"`
}

type engineRegistrationBody struct {
	Name        string                 `json:"name"`
	Description *string                `json:"description,omitempty"`
	PublicKeys  registrationPublicKeys `json:"publicKeys"`
}

type signedRequestBundle struct {
	Method         string `json:"method"`
	URL            string `json:"url"`
	Body           string `json:"body"`
	ContentType    string `json:"contentType"`
	ContentDigest  string `json:"contentDigest"`
	IdempotencyKey string `json:"idempotencyKey"`
	SignatureInput string `json:"signatureInput"`
	Signature      string `json:"signature"`
}

func configCmd(_ execs, _ *cmdConfiguration) (*cobra.Command, error) {
	command := &cobra.Command{
		Use:   "config",
		Short: "manage engine credentials and registration material",
		// Config maintenance must not initialize the backup runtime or require
		// an operational --config-file.
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
	}

	credentials := &cobra.Command{Use: "credentials", Short: "manage private engine credentials"}
	credentials.AddCommand(generateCredentialsCmd())
	command.AddCommand(
		credentials,
		publicInfoCmd(),
		registrationBundleCmd(),
		rotateAuthenticationCmd(),
		rotateConfigurationKeyCmd(),
	)
	return command, nil
}

func generateCredentialsCmd() *cobra.Command {
	var output string
	var force bool
	command := &cobra.Command{
		Use:   "generate",
		Short: "generate new private engine credentials",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			seed := make([]byte, 32)
			if _, err := io.ReadFull(rand.Reader, seed); err != nil {
				return errors.New("unable to generate credential seed")
			}
			credentials := api.EngineCredentials{
				Version:                           api.DatabackerCredentialsV2,
				Seed:                              base64.StdEncoding.EncodeToString(seed),
				AuthenticationGeneration:          1,
				ConfigurationEncryptionGeneration: 1,
			}
			return writeCredentials(command, output, force, credentials)
		},
	}
	command.Flags().StringVarP(&output, "output", "o", "", "write credentials to this file instead of stdout")
	command.Flags().BoolVar(&force, "force", false, "overwrite an existing output file")
	return command
}

func publicInfoCmd() *cobra.Command {
	var credentialsFile string
	command := &cobra.Command{
		Use:   "public-info",
		Short: "print public keys and deterministic key IDs",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			credentials, err := readCredentials(credentialsFile)
			if err != nil {
				return err
			}
			engineIdentity, err := identity.New(credentials)
			if err != nil {
				return fmt.Errorf("invalid credentials: %w", err)
			}
			info, err := makePublicInfo(engineIdentity)
			if err != nil {
				return err
			}
			return writeJSON(command.OutOrStdout(), info)
		},
	}
	command.Flags().StringVar(&credentialsFile, "credentials-file", "", "YAML credentials file created by config credentials generate")
	_ = command.MarkFlagRequired("credentials-file")
	return command
}

func registrationBundleCmd() *cobra.Command {
	var options requestBundleOptions
	command := &cobra.Command{
		Use:   "registration-bundle",
		Short: "create a signed engine-registration request bundle",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			credentials, err := readCredentials(options.credentialsFile)
			if err != nil {
				return err
			}
			bundle, err := makeSignedRequestBundle(credentials, http.MethodPost, options)
			if err != nil {
				return err
			}
			return writeJSON(command.OutOrStdout(), bundle)
		},
	}
	addRequestBundleFlags(command, &options, false)
	return command
}

func rotateAuthenticationCmd() *cobra.Command {
	var options requestBundleOptions
	var output string
	var force bool
	command := &cobra.Command{
		Use:   "rotate-authentication",
		Short: "prepare the next authentication generation and signed update",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			credentials, err := readCredentials(options.credentialsFile)
			if err != nil {
				return err
			}
			if credentials.AuthenticationGeneration == ^uint64(0) {
				return errors.New("authentication generation cannot be incremented")
			}
			if err := validateRotationOutput(options.credentialsFile, output); err != nil {
				return err
			}
			credentials.AuthenticationGeneration++
			bundle, err := makeSignedRequestBundle(credentials, http.MethodPatch, options)
			if err != nil {
				return err
			}
			if err := writeCredentials(command, output, force, credentials); err != nil {
				return err
			}
			return writeJSON(command.OutOrStdout(), bundle)
		},
	}
	addRequestBundleFlags(command, &options, true)
	command.Flags().StringVarP(&output, "output", "o", "", "write the proposed credentials to this new file")
	command.Flags().BoolVar(&force, "force", false, "overwrite an existing output file")
	_ = command.MarkFlagRequired("output")
	return command
}

func rotateConfigurationKeyCmd() *cobra.Command {
	var options requestBundleOptions
	var output string
	var force bool
	command := &cobra.Command{
		Use:   "rotate-configuration-key",
		Short: "prepare the next configuration-key generation and signed update",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			credentials, err := readCredentials(options.credentialsFile)
			if err != nil {
				return err
			}
			if credentials.ConfigurationEncryptionGeneration == ^uint64(0) {
				return errors.New("configuration-key generation cannot be incremented")
			}
			if err := validateRotationOutput(options.credentialsFile, output); err != nil {
				return err
			}
			previous := credentials.ConfigurationEncryptionGeneration
			credentials.ConfigurationEncryptionGeneration++
			retained := []uint64(nil)
			if credentials.RetainedConfigurationEncryptionGenerations != nil {
				retained = append(retained, (*credentials.RetainedConfigurationEncryptionGenerations)...)
			}
			retained = append(retained, previous)
			credentials.RetainedConfigurationEncryptionGenerations = &retained
			bundle, err := makeSignedRequestBundle(credentials, http.MethodPatch, options)
			if err != nil {
				return err
			}
			if err := writeCredentials(command, output, force, credentials); err != nil {
				return err
			}
			_, _ = fmt.Fprintln(command.ErrOrStderr(), "The previous configuration key remains retained. Remove it only after all configurations have been re-encrypted for the new key.")
			return writeJSON(command.OutOrStdout(), bundle)
		},
	}
	addRequestBundleFlags(command, &options, true)
	command.Flags().StringVarP(&output, "output", "o", "", "write the proposed credentials to this new file")
	command.Flags().BoolVar(&force, "force", false, "overwrite an existing output file")
	_ = command.MarkFlagRequired("output")
	return command
}

type requestBundleOptions struct {
	credentialsFile string
	endpoint        string
	name            string
	description     string
	idempotencyKey  string
}

func addRequestBundleFlags(command *cobra.Command, options *requestBundleOptions, update bool) {
	command.Flags().StringVar(&options.credentialsFile, "credentials-file", "", "YAML credentials file")
	command.Flags().StringVar(&options.endpoint, "url", "", "exact Cloud engine registration or update URL")
	command.Flags().StringVar(&options.name, "name", "", "engine name")
	command.Flags().StringVar(&options.description, "description", "", "engine description")
	command.Flags().StringVar(&options.idempotencyKey, "idempotency-key", "", "canonical UUID to reuse for a retry; generated when omitted")
	_ = command.MarkFlagRequired("credentials-file")
	_ = command.MarkFlagRequired("url")
	_ = command.MarkFlagRequired("name")
	if update {
		command.Flags().Lookup("url").Usage = "exact Cloud engine update URL"
	}
}

func makeSignedRequestBundle(credentials api.EngineCredentials, method string, options requestBundleOptions) (signedRequestBundle, error) {
	if options.name == "" {
		return signedRequestBundle{}, errors.New("engine name must not be empty")
	}
	engineIdentity, err := identity.New(credentials)
	if err != nil {
		return signedRequestBundle{}, fmt.Errorf("invalid credentials: %w", err)
	}
	endpoint, err := url.Parse(options.endpoint)
	if err != nil || endpoint.Host == "" || endpoint.Scheme != "http" && endpoint.Scheme != "https" || endpoint.User != nil || endpoint.Fragment != "" {
		return signedRequestBundle{}, errors.New("URL must be an absolute HTTP or HTTPS URL without user information or a fragment")
	}
	info, err := makePublicInfo(engineIdentity)
	if err != nil {
		return signedRequestBundle{}, err
	}
	body := engineRegistrationBody{
		Name: options.name,
		PublicKeys: registrationPublicKeys{
			Authentication:          registrationPublicKey{Algorithm: info.Authentication.Algorithm, Generation: info.Authentication.Generation, PublicKey: info.Authentication.PublicKey},
			ConfigurationEncryption: registrationPublicKey{Algorithm: info.ConfigurationEncryption.Algorithm, Generation: info.ConfigurationEncryption.Generation, PublicKey: info.ConfigurationEncryption.PublicKey},
		},
	}
	if options.description != "" {
		body.Description = &options.description
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return signedRequestBundle{}, errors.New("unable to encode registration body")
	}
	request, err := http.NewRequest(method, endpoint.String(), bytes.NewReader(bodyBytes))
	if err != nil {
		return signedRequestBundle{}, errors.New("unable to construct registration request")
	}
	request.Header.Set("Content-Type", "application/json")
	idempotencyKey := options.idempotencyKey
	if idempotencyKey == "" {
		idempotencyKey = uuid.NewString()
	}
	request.Header.Set("Idempotency-Key", idempotencyKey)
	if err := remote.SignRegistrationRequest(request, engineIdentity); err != nil {
		return signedRequestBundle{}, fmt.Errorf("unable to sign registration request: %w", err)
	}
	return signedRequestBundle{
		Method:         method,
		URL:            endpoint.String(),
		Body:           string(bodyBytes),
		ContentType:    request.Header.Get("Content-Type"),
		ContentDigest:  request.Header.Get("Content-Digest"),
		IdempotencyKey: request.Header.Get("Idempotency-Key"),
		SignatureInput: request.Header.Get("Signature-Input"),
		Signature:      request.Header.Get("Signature"),
	}, nil
}

func validateRotationOutput(input, output string) error {
	inputPath, err := filepath.Abs(input)
	if err != nil {
		return errors.New("unable to resolve credentials input path")
	}
	outputPath, err := filepath.Abs(output)
	if err != nil {
		return errors.New("unable to resolve credentials output path")
	}
	if inputPath == outputPath {
		return errors.New("rotation output must be different from the active credentials file")
	}
	return nil
}

func makePublicInfo(engineIdentity *identity.Identity) (publicInfo, error) {
	configurationGeneration := engineIdentity.ConfigurationGeneration()
	configurationPublicKey, err := engineIdentity.ConfigurationPublicKey(configurationGeneration)
	if err != nil {
		return publicInfo{}, errors.New("unable to derive configuration public key")
	}
	configurationKeyID, err := engineIdentity.ConfigurationKeyID(configurationGeneration)
	if err != nil {
		return publicInfo{}, errors.New("unable to derive configuration key ID")
	}
	return publicInfo{
		Authentication: publicKeyInfo{
			Algorithm:  "ed25519",
			Generation: engineIdentity.AuthenticationGeneration(),
			PublicKey:  base64.StdEncoding.EncodeToString(engineIdentity.AuthenticationPublicKey()),
			KeyID:      engineIdentity.AuthenticationKeyID(),
		},
		ConfigurationEncryption: publicKeyInfo{
			Algorithm:  "x25519",
			Generation: configurationGeneration,
			PublicKey:  base64.StdEncoding.EncodeToString(configurationPublicKey),
			KeyID:      configurationKeyID,
		},
	}, nil
}

func readCredentials(path string) (api.EngineCredentials, error) {
	var credentials api.EngineCredentials
	file, err := os.Open(path)
	if err != nil {
		return credentials, fmt.Errorf("unable to open credentials file: %w", err)
	}
	defer func() { _ = file.Close() }()
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	if err := decoder.Decode(&credentials); err != nil {
		return credentials, fmt.Errorf("unable to decode credentials file: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return credentials, errors.New("credentials file contains multiple documents")
	}
	return credentials, nil
}

func writeCredentials(command *cobra.Command, path string, force bool, credentials api.EngineCredentials) error {
	encoded, err := yaml.Marshal(credentials)
	if err != nil {
		return errors.New("unable to encode credentials")
	}
	if path == "" {
		_, err = command.OutOrStdout().Write(encoded)
		return err
	}
	if !force {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return fmt.Errorf("unable to create credentials file: %w", err)
		}
		if err := writeCredentialBytes(file, encoded); err != nil {
			_ = file.Close()
			_ = os.Remove(path)
			return err
		}
		return file.Close()
	}

	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return fmt.Errorf("unable to create temporary credentials file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("unable to secure credentials file: %w", err)
	}
	if err := writeCredentialBytes(temporary, encoded); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("unable to close credentials file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("unable to replace credentials file: %w", err)
	}
	return nil
}

func writeCredentialBytes(file *os.File, encoded []byte) error {
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("unable to secure credentials file: %w", err)
	}
	if _, err := file.Write(encoded); err != nil {
		return fmt.Errorf("unable to write credentials file: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("unable to sync credentials file: %w", err)
	}
	return nil
}

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
