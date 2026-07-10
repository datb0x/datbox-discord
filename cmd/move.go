package cmd

import (
	"datbox/comm"
	"log"

	"github.com/spf13/cobra"
)

var (
	moveCmd = &cobra.Command{
		Use:   "mv <src> <dest>",
		Short: "Move a file or directory",
		Args:  cobra.MinimumNArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			client, err := comm.NewClientWrapperAndConnect()
			if err != nil {
				log.Fatalln(err)
			}
			defer client.Close()
			writer := client.Prepare()
			writer.WriteUtf8(args[0])
			writer.WriteUtf8(args[1])
			err = client.Send(MSG_TYPE_MOVE)
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
