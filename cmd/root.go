package cmd

import (
	"github.com/spf13/cobra"
)

var (
	rootCmd = &cobra.Command{
		Use:   "datbox",
		Short: "Use Discord as a storage service",
	}
)

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize()

	rootCmd.AddCommand(serverCmd)
	rootCmd.AddCommand(uploadCmd)
	rootCmd.AddCommand(downloadCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(moveCmd)
	rootCmd.AddCommand(removeCmd)
	rootCmd.AddCommand(mkdirCmd)
	rootCmd.AddCommand(copyCmd)
	rootCmd.AddCommand(infoCmd)
}
