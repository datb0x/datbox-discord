package cmd

import (
	"datbox/comm"
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
			client, id, err := comm.StartClientAndWait()
			if err != nil {
				log.Fatalln(err)
			}
			writer := comm.NewWriter()
			writer.WriteInt32(id)
			writer.WriteUtf8(args[0])
			writer.WriteUtf8(args[1])
			writer.WriterBool(copyRecursive)
			err = client.Write(MSG_TYPE_COPY, writer.Data)
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
	copyCmd.Flags().BoolVarP(&copyRecursive, "recursive", "r", false, "Copy files recursively")
}
