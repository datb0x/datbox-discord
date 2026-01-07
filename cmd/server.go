package cmd

import (
	"datbox/server"
	"log"

	"github.com/adrg/xdg"
	"github.com/spf13/cobra"
)

var (
	channelId   string
	configPath  string
	concurrency int
	dataDir     string
	token       string
	serverCmd   = &cobra.Command{
		Use:   "server",
		Short: "Run the datbox local server",
		RunE: func(cmd *cobra.Command, args []string) error {
			config := server.NewConfig(configPath, channelId, dataDir, concurrency, token)
			if err := config.Load(); err != nil {
				return err
			}
			if err := config.Save(); err != nil {
				return err
			}
			return nil
		},
	}
)

func init() {
	configFilePath, err := xdg.ConfigFile("datbox/config.json")
	if err != nil {
		log.Fatalf("Error getting config path: %v", err)
	}

	serverCmd.PersistentFlags().StringVarP(&channelId, "channel", "c", "", "ID of the text channel where chunks will be stored")
	serverCmd.PersistentFlags().StringVarP(&configPath, "config", "C", configFilePath, "Local path to config file")
	serverCmd.PersistentFlags().IntVarP(&concurrency, "concurrency", "m", 10, "Maximum number upload and download jobs that can run in parallel")
	serverCmd.PersistentFlags().StringVarP(&dataDir, "data-dir", "d", "", "Directory where data should be stored")
	serverCmd.PersistentFlags().StringVarP(&token, "token", "t", "", "Discord bot token. This option not recommended. Use .env or config instead")
}
