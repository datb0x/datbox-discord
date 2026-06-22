package cmd

import (
	"datbox/comm"
	"log"

	"github.com/spf13/cobra"
)

var (
	infoCmd = &cobra.Command{
		Use:   "info",
		Short: "Get info of Datbox",
		Run: func(cmd *cobra.Command, args []string) {
			client, id, err := comm.StartClientAndWait()
			if err != nil {
				log.Fatalln(err)
			}
			defer client.Close()
			writer := comm.NewWriter()
			writer.WriteInt32(id)
			err = client.Write(MSG_TYPE_INFO, writer.Data)
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
