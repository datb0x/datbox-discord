package cmd

import (
	"datbox/comm"
	"log"
	"math/rand/v2"

	ipc "github.com/james-barrow/golang-ipc"
	"github.com/spf13/cobra"
)

var (
	long    = false
	human   = false
	listCmd = &cobra.Command{
		Use:   "ls",
		Short: "List files in a directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := ipc.StartClient("datbox", nil)
			if err != nil {
				return err
			}
			for message, err := client.Read(); err != nil || message.MsgType == -1; {
				if err != nil {
					return err
				}
				if message.Err != nil {
					return message.Err
				}
				if client.StatusCode() == ipc.Connected {
					break
				}
			}
			log.Println("Connected")
			id := rand.Int32()
			for id >= -1 && id <= 1 {
				id = rand.Int32()
			}
			writer := comm.NewWriter()
			writer.WriteInt32(id)
			if long {
				writer.WriteByte(1)
			} else {
				writer.WriteByte(0)
			}
			if human {
				writer.WriteByte(1)
			} else {
				writer.WriteByte(0)
			}
			if len(args) > 0 {
				writer.WriteUtf8(args[0])
			}
			err = client.Write(MSG_TYPE_LIST, writer.Data)
			if err != nil {
				return err
			}
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
				}
			}
			return nil
		},
	}
)

func init() {
	listCmd.Flags().BoolVarP(&long, "long", "l", false, "Use a long listing format")
	listCmd.Flags().BoolVarP(&human, "human", "H", false, "Human readable file size")
}
