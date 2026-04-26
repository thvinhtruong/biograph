package cli

import (
	"fmt"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string
var verbose bool

var rootCmd = &cobra.Command{
	Use:   "biograph",
	Short: "BioGraph — AI study copilot for lecture PDFs",
	Long: `BioGraph ingests lecture PDFs, synthesises First Thoughts study ledgers,
and lets you ask questions or run interactive quiz sessions from your terminal.`,
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ./biograph.yaml)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose logging")

	rootCmd.AddCommand(ingestCmd)
	rootCmd.AddCommand(askCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(quizCmd)
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigName("biograph")
		viper.SetConfigType("yaml")
		viper.AddConfigPath(".")
		viper.AddConfigPath("$HOME")
	}

	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil {
		if verbose {
			log.Info().Str("file", viper.ConfigFileUsed()).Msg("config loaded")
		}
	}

	// Configure logger
	if verbose {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
}
