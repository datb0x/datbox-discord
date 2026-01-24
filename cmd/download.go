package cmd

import (
	"datbox/comm"
	"fmt"
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
			client.Write(MSG_TYPE_DOWNLOAD, writer.Data)
			for {
				message, err := client.Read()
				if err != nil {
					log.Fatalln(err)
				}
				if message.MsgType != int(id) {
					continue
				}
				reader := comm.NewReader(message)
				status, err := reader.ReadByte()
				if err != nil {
					log.Fatalln(err)
				}
				if status == 0 || status == 1 {
					str, err := reader.ReadUtf8()
					if err != nil {
						log.Fatalln(err)
					}
					fmt.Println("\n" + str)
					client.Close()
					break
				} else if status == 2 {
					progress, err := reader.ReadFloat32()
					if err != nil {
						log.Fatalln(err)
					}
					fmt.Printf("\rProgress: %03d%%", int(100*progress))
				}
			}
		},
	}
)
