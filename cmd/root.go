package cmd

import (
	"log"

	"github.com/adrg/xdg"
	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/cobra"
)

var (
	config  = koanf.New(".")
	rootCmd = &cobra.Command{
		Use:   "datbox",
		Short: "Use Discord as a storage service",
	}
)

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.AddCommand(serverCmd)
}

func initConfig() {
	configFilePath, err := xdg.ConfigFile("datbox/config.json")
	if err != nil {
		log.Fatalf("Error getting config path: %v", err)
	}

	err = config.Load(file.Provider(configFilePath), json.Parser())
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}
}
