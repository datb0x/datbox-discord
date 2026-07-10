package cmd

import (
	"datbox/util"
	"log"

	"github.com/spf13/cobra"
)

var (
	recursive = false
	remote    = false
	removeCmd = &cobra.Command{
		Use:   "rm <virtualPath>",
		Short: "Remove a file or directory (recursively)",
		Args:  cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			client, err := util.NewClientWrapperAndConnect()
			if err != nil {
				log.Fatalln(err)
			}
			defer client.Close()
			writer := client.Prepare()
			writer.WriteUtf8(args[0])
			writer.WriterBool(recursive)
			writer.WriterBool(remote)
			err = client.Send(MSG_TYPE_REMOVE)
			if err != nil {
				log.Println("Failed to write data to ipc")
				log.Fatalln(err)
			}
			err = client.ReadAllMsgs()
			if err != nil {
				log.Fatalln(err)
			}
		},
	}
)

func init() {
	removeCmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "Remove files recursively")
	removeCmd.Flags().BoolVarP(&remote, "remote", "R", false, "Delete the remote attachments")
}
