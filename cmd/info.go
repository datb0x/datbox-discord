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
			client, err := comm.NewClientWrapperAndConnect()
			if err != nil {
				log.Fatalln(err)
			}
			defer client.Close()
			client.Prepare()
			err = client.Send(MSG_TYPE_INFO)
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
