package cmd

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/databacker/api/go/api"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/databacker/mysql-backup/pkg/config"
	"github.com/databacker/mysql-backup/pkg/core"
	"github.com/databacker/mysql-backup/pkg/database"
	"github.com/databacker/mysql-backup/pkg/remote"
	"github.com/databacker/mysql-backup/pkg/storage/credentials"
)

type execs interface {
	SetLogger(logger *log.Logger)
	GetLogger() *log.Logger
	Dump(ctx context.Context, opts core.DumpOptions) (core.DumpResults, error)
	Restore(ctx context.Context, opts core.RestoreOptions) error
	Prune(ctx context.Context, opts core.PruneOptions) error
	Timer(timerOpts core.TimerOptions, cmd func() error) error
}

type subCommand func(execs, *cmdConfiguration) (*cobra.Command, error)

var subCommands = []subCommand{dumpCmd, restoreCmd, pruneCmd}

type cmdConfiguration struct {
	dbconn        database.Connection
	creds         credentials.Creds
	configuration *api.ConfigSpec
	logger        *log.Logger
}

const (
	defaultPort = 3306
)

func rootCmd(execs execs) (*cobra.Command, error) {
	var (
		v         *viper.Viper
		cmd       *cobra.Command
		cmdConfig = &cmdConfiguration{}
		ctx       = context.Background()
	)
	cmd = &cobra.Command{
		Use:   "mysql-backup",
		Short: "backup or restore one or more mysql-compatible databases",
		Long: `Backup or restore one or more mysql-compatible databases.
		In addition to the provided command-line flag options and environment variables,
		when using s3-storage, supports the following AWS options:
		
		AWS_ACCESS_KEY_ID: AWS Key ID
		AWS_SECRET_ACCESS_KEY: AWS Secret Access Key
		AWS_REGION: Region in which the bucket resides
		AWS_ENDPOINT_URL: Endpoint URL to use instead of default s3.<region>.amazonaws.com
		AWS_PATH_STYLE: Use path-style URLs for S3 requests instead of virtual-hosted-style URLs
		`,
		PersistentPreRunE: func(c *cobra.Command, args []string) error {
			bindFlags(cmd, v)
			var logger = log.New()
			logLevel := v.GetInt("verbose")
			debugSet := v.IsSet("debug")
			if !v.IsSet("verbose") && (v.GetBool("debug") || (debugSet && v.GetString("debug") == "true")) {
				logLevel = 1
			}
			switch logLevel {
			case 0:
				logger.SetLevel(log.InfoLevel)
			case 1:
				logger.SetLevel(log.DebugLevel)
			case 2:
				logger.SetLevel(log.TraceLevel)
			}

			// read the config file, if needed; the structure of the config differs quite some
			// from the necessarily flat env vars/CLI flags, so we can't just use viper's
			// automatic config file support.
			var (
				actualConfig    *api.ConfigSpec
				tracerExporters []sdktrace.SpanExporter
			)

			if configFilePath := v.GetString("config-file"); configFilePath != "" {
				var (
					f   *os.File
					err error
				)
				if f, err = os.Open(configFilePath); err != nil {
					return fmt.Errorf("fatal error config file: %w", err)
				}
				defer f.Close()
				actualConfig, err = config.ProcessConfig(f)
				if err != nil {
					return fmt.Errorf("unable to read provided config: %w", err)
				}
			}

			// the structure of our config file is more complex and with relationships than our config/env var
			// so we cannot use a single viper structure, as described above.

			// set up database connection
			if actualConfig != nil {
				if actualConfig.Database != nil {
					if actualConfig.Database.Server != nil && *actualConfig.Database.Server != "" {
						cmdConfig.dbconn.Host = *actualConfig.Database.Server
					}
					if actualConfig.Database.Port != nil && *actualConfig.Database.Port != 0 {
						cmdConfig.dbconn.Port = *actualConfig.Database.Port
					}
					if actualConfig.Database.Credentials.Username != nil && *actualConfig.Database.Credentials.Username != "" {
						cmdConfig.dbconn.User = *actualConfig.Database.Credentials.Username
					}
					if actualConfig.Database.Credentials.Password != nil && *actualConfig.Database.Credentials.Password != "" {
						cmdConfig.dbconn.Pass = *actualConfig.Database.Credentials.Password
					}
				}
				cmdConfig.configuration = actualConfig

				if actualConfig.Telemetry != nil && actualConfig.Telemetry.URL != nil && *actualConfig.Telemetry.URL != "" {

					// set up telemetry with tracing
					u, err := url.Parse(*actualConfig.Telemetry.URL)
					if err != nil {
						return fmt.Errorf("invalid telemetry URL: %w", err)
					}
					tlsConfig, err := remote.GetTLSConfig(u.Hostname(), *actualConfig.Telemetry.Certificates, *actualConfig.Telemetry.Credentials)
					if err != nil {
						return fmt.Errorf("unable to set up telemetry: %w", err)
					}

					tracerExporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpoint(*actualConfig.Telemetry.URL), otlptracehttp.WithTLSClientConfig(tlsConfig))
					if err != nil {
						return fmt.Errorf("unable to set up telemetry: %w", err)
					}
					tracerExporters = append(tracerExporters, tracerExporter)
				}
			}

			// override config with env var or CLI flag, if set
			dbHost := v.GetString("server")
			if dbHost != "" && v.IsSet("server") {
				cmdConfig.dbconn.Host = dbHost
			}
			dbPort := v.GetInt("port")
			if dbPort != 0 && (v.IsSet("port") || cmdConfig.dbconn.Port == 0) {
				cmdConfig.dbconn.Port = dbPort
			}
			dbUser := v.GetString("user")
			if dbUser != "" && v.IsSet("user") {
				cmdConfig.dbconn.User = dbUser
			}
			dbPass := v.GetString("pass")
			if dbPass != "" && v.IsSet("pass") {
				cmdConfig.dbconn.Pass = dbPass
			}

			// these are not from the config file, as they are generic credentials, used across all targets.
			// the config file uses specific ones per target
			cmdConfig.creds = credentials.Creds{
				AWS: credentials.AWSCreds{
					Endpoint:        v.GetString("aws-endpoint-url"),
					PathStyle:       v.GetBool("aws-path-style"),
					AccessKeyID:     v.GetString("aws-access-key-id"),
					SecretAccessKey: v.GetString("aws-secret-access-key"),
					Region:          v.GetString("aws-region"),
				},
				SMB: credentials.SMBCreds{
					Username: v.GetString("smb-user"),
					Password: v.GetString("smb-pass"),
					Domain:   v.GetString("smb-domain"),
				},
			}
			cmdConfig.logger = logger

			if v.GetBool("trace-stderr") {
				exp, err := stdouttrace.New(stdouttrace.WithPrettyPrint(), stdouttrace.WithWriter(os.Stderr))
				if err != nil {
					return fmt.Errorf("failed to initialize stdouttrace exporter: %w", err)
				}
				tracerExporters = append(tracerExporters, exp)
			}
			var tracerProviderOpts []sdktrace.TracerProviderOption
			for _, exp := range tracerExporters {
				tracerProviderOpts = append(tracerProviderOpts, sdktrace.WithBatcher(exp))
			}
			otel.SetTracerProvider(sdktrace.NewTracerProvider(tracerProviderOpts...))

			return nil
		},
	}

	v = viper.New()
	v.SetEnvPrefix("db")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv()

	// server hostname via CLI or env var
	pflags := cmd.PersistentFlags()
	pflags.String("server", "", "hostname for database server")

	pflags.String("config-file", "", "config file to use, if any; individual CLI flags override config file")

	// server port via CLI or env var or default
	pflags.Int("port", defaultPort, "port for database server")

	// user via CLI or env var
	pflags.String("user", "", "username for database server")

	// pass via CLI or env var
	pflags.String("pass", "", "password for database server")

	// debug via CLI or env var or default
	pflags.IntP("verbose", "v", 0, "set log level, 1 is debug, 2 is trace")
	pflags.Bool("debug", false, "set log level to debug, equivalent of --verbose=1; if both set, --version always overrides")
	pflags.Bool("trace-stderr", false, "trace to stderr, in addition to any configured telemetry")

	// aws options
	pflags.String("aws-endpoint-url", "", "Specify an alternative endpoint for s3 interoperable systems e.g. Digitalocean; ignored if not using s3.")
	pflags.Bool("aws-path-style", false, "Use path-style addressing of buckets instead of default virtual-host-style; ignored if not using s3.")
	pflags.String("aws-access-key-id", "", "Access Key for s3 and s3 interoperable systems; ignored if not using s3.")
	pflags.String("aws-secret-access-key", "", "Secret Access Key for s3 and s3 interoperable systems; ignored if not using s3.")
	pflags.String("aws-region", "", "Region for s3 and s3 interoperable systems; ignored if not using s3.")

	// smb options
	pflags.String("smb-user", "", "SMB username")
	pflags.String("smb-pass", "", "SMB username")
	pflags.String("smb-domain", "", "SMB domain")

	for _, subCmd := range subCommands {
		if sc, err := subCmd(execs, cmdConfig); err != nil {
			return nil, err
		} else {
			cmd.AddCommand(sc)
		}
	}

	return cmd, nil
}

// Bind each cobra flag to its associated viper configuration (config file and environment variable)
func bindFlags(cmd *cobra.Command, v *viper.Viper) {
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		// Determine the naming convention of the flags when represented in the config file
		configName := f.Name
		_ = v.BindPFlag(configName, f)
		// Apply the viper config value to the flag when the flag is not set and viper has a value
		if !f.Changed && v.IsSet(configName) {
			val := v.Get(configName)
			_ = cmd.Flags().Set(f.Name, fmt.Sprintf("%v", val))
		}
	})
}

// Execute primary function for cobra
func Execute() {
	rootCmd, err := rootCmd(nil)
	if err != nil {
		log.Fatal(err)
	}
	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
