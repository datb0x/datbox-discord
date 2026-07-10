package cmd

import (
	"datbox/util"
	"log"

	"github.com/spf13/cobra"
)

var (
	copyRecursive = false
	copyCmd       = &cobra.Command{
		Use:   "cp <src> <dest>",
		Short: "Copy a file or directory (recursively)",
		Args:  cobra.MinimumNArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			client, err := util.NewClientWrapperAndConnect()
			if err != nil {
				log.Fatalln(err)
			}
			defer client.Close()
			writer := client.Prepare()
			writer.WriteUtf8(args[0])
			writer.WriteUtf8(args[1])
			writer.WriterBool(copyRecursive)
			err = client.Send(MSG_TYPE_COPY)
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
	copyCmd.Flags().BoolVarP(&copyRecursive, "recursive", "r", false, "Copy files recursively")
}
