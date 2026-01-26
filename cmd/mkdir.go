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
			client, id, err := comm.StartClientAndWait()
			if err != nil {
				log.Fatalln(err)
			}
			writer := comm.NewWriter()
			writer.WriteInt32(id)
			writer.WriteUtf8(args[0])
			writer.WriterBool(recursive)
			writer.WriterBool(remote)
			err = client.Write(MSG_TYPE_MKDIR, writer.Data)
			if err != nil {
				log.Println("Failed to write data to ipc")
				log.Fatalln(err)
			}
			err = comm.ReadUntilEnd(client, id)
			if err != nil {
				log.Fatalln(err)
			}
		},
	}
)

func init() {
	mkdirCmd.Flags().BoolVarP(&mkdirRecursive, "recursive", "r", false, "Create parents if not exists")
}
