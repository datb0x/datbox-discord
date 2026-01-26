package cmd

import (
	"datbox/comm"
	"log"

	"github.com/spf13/cobra"
)

var (
	downloadCmd = &cobra.Command{
		Use:   "download <virtual-path> <physical-path>",
		Short: "Download a file from the virtual file system",
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
			err = client.Write(MSG_TYPE_DOWNLOAD, writer.Data)
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
