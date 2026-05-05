package cmd

import (
	"fmt"
	"os"

	"github.com/btopcu/argus/cmd/argusctl/internal/client"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	Version   = "dev"
	GitSHA    = "unknown"
	BuildTime = "unknown"
)

var (
	cfgFile   string
	apiURL    string
	token     string
	certFile  string
	keyFile   string
	caFile    string
	outputFmt string
)

var rootCmd = &cobra.Command{
	Use:   "argusctl",
	Short: "Argus administrative CLI",
	Long: `argusctl is the administrative command-line client for the Argus platform.

It talks to the Argus HTTP API to manage tenants, API keys, users, and to
inspect service health. Authentication is either a JWT bearer token
(--token) or mTLS client certificate (--cert/--key/--ca).`,
	SilenceUsage:  true,
	SilenceErrors: false,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default $HOME/.argusctl.yaml)")
	rootCmd.PersistentFlags().StringVar(&apiURL, "api-url", "http://localhost:8084", "Argus API base URL")
	rootCmd.PersistentFlags().StringVar(&token, "token", "", "JWT bearer token for API authentication")
	rootCmd.PersistentFlags().StringVar(&certFile, "cert", "", "mTLS client certificate (PEM)")
	rootCmd.PersistentFlags().StringVar(&keyFile, "key", "", "mTLS client key (PEM)")
	rootCmd.PersistentFlags().StringVar(&caFile, "ca", "", "CA certificate bundle for server verification (PEM)")
	rootCmd.PersistentFlags().StringVar(&outputFmt, "format", "table", "output format: table|json")

	_ = viper.BindPFlag("api_url", rootCmd.PersistentFlags().Lookup("api-url"))
	_ = viper.BindPFlag("token", rootCmd.PersistentFlags().Lookup("token"))
	_ = viper.BindPFlag("cert", rootCmd.PersistentFlags().Lookup("cert"))
	_ = viper.BindPFlag("key", rootCmd.PersistentFlags().Lookup("key"))
	_ = viper.BindPFlag("ca", rootCmd.PersistentFlags().Lookup("ca"))
	_ = viper.BindPFlag("format", rootCmd.PersistentFlags().Lookup("format"))

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(tenantCmd)
	rootCmd.AddCommand(apikeyCmd)
	rootCmd.AddCommand(userCmd)
	rootCmd.AddCommand(healthCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(simCmd)
	rootCmd.AddCommand(backupCmd)
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		if err == nil {
			viper.AddConfigPath(home)
			viper.SetConfigName(".argusctl")
			viper.SetConfigType("yaml")
		}
	}

	viper.SetEnvPrefix("ARGUSCTL")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil {
		// config loaded; viper overrides unset flags via defaults below
	}

	if apiURL == "http://localhost:8084" && viper.IsSet("api_url") {
		apiURL = viper.GetString("api_url")
	}
	if token == "" {
		token = viper.GetString("token")
	}
	if certFile == "" {
		certFile = viper.GetString("cert")
	}
	if keyFile == "" {
		keyFile = viper.GetString("key")
	}
	if caFile == "" {
		caFile = viper.GetString("ca")
	}
	if outputFmt == "table" && viper.IsSet("format") {
		outputFmt = viper.GetString("format")
	}
}

// newClient builds a client from current flag/config state.
func newClient() (*client.Client, error) {
	if certFile != "" || keyFile != "" {
		if certFile == "" || keyFile == "" {
			return nil, fmt.Errorf("both --cert and --key are required for mTLS")
		}
	}
	return client.New(client.Config{
		BaseURL:  apiURL,
		Token:    token,
		CertFile: certFile,
		KeyFile:  keyFile,
		CAFile:   caFile,
	})
}

// errExit writes an error to stderr in a user-friendly format.
func errExit(err error) error {
	if err == nil {
		return nil
	}
	if apiErr, ok := err.(*client.APIError); ok {
		return fmt.Errorf("API error [%s]: %s", apiErr.Code, apiErr.Message)
	}
	return err
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print argusctl version information",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("argusctl %s (git %s, built %s)\n", Version, GitSHA, BuildTime)
		return nil
	},
}
