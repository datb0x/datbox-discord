package cmd

import (
	"datbox/comm"
	"log"
	"math/rand/v2"

	ipc "github.com/james-barrow/golang-ipc"
	"github.com/spf13/cobra"
)

var (
	downloadCmd = &cobra.Command{
		Use:   "download <virtual-path> <physical-path>",
		Short: "Download a file from the virtual file system",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := ipc.StartClient("datbox", nil)
			if err != nil {
				return err
			}
			id := rand.Int32()
			for id == 0 || id == -1 {
				id = rand.Int32()
			}
			writer := comm.NewWriter()
			writer.WriteInt32(id)
			writer.WriteUtf8(args[0])
			writer.WriteUtf8(args[1])
			client.Write(MSG_TYPE_DOWNLOAD, writer.Data)
			for {
				message, err := client.Read()
				if err != nil {
					return err
				}
				if message.MsgType != int(id) {
					continue
				}
				reader := comm.NewReader(message)
				status, err := reader.ReadByte()
				if err != nil {
					return err
				}
				if status == 0 || status == 1 {
					str, err := reader.ReadUtf8()
					if err != nil {
						return err
					}
					log.Println("\n" + str)
					client.Close()
					break
				} else if status == 2 {
					current, err := reader.ReadUInt64()
					if err != nil {
						return err
					}
					total, err := reader.ReadUInt64()
					if err != nil {
						return err
					}
					log.Printf("\rProgress: %03d%%", current/total)
				}
			}
			return nil
		},
	}
)
