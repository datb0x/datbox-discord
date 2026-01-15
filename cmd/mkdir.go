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
			_, status, err := comm.WaitForMessage(client, id)
			if err != nil {
				log.Println("Failed to read data from ipc")
				log.Fatalln(err)
			}
			if status == 0 || status == 1 {
				if err != nil {
					log.Fatalln(err)
				}
				client.Close()
			}
		},
	}
)

func init() {
	mkdirCmd.Flags().BoolVarP(&mkdirRecursive, "recursive", "r", false, "Create parents if not exists")
}
