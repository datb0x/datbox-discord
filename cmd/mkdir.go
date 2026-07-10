package cmd

import (
	"datbox/comm"
	"log"

	"github.com/spf13/cobra"
)

var (
	mkdirRecursive = false
	mkdirCmd       = &cobra.Command{
		Use:   "mkdir <virtualPath>",
		Short: "Create a directory",
		Args:  cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			client, err := comm.NewClientWrapperAndConnect()
			if err != nil {
				log.Fatalln(err)
			}
			defer client.Close()
			writer := client.Prepare()
			writer.WriteUtf8(args[0])
			writer.WriterBool(recursive)
			writer.WriterBool(remote)
			err = client.Send(MSG_TYPE_MKDIR)
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
	mkdirCmd.Flags().BoolVarP(&mkdirRecursive, "recursive", "r", false, "Create parents if not exists")
}
