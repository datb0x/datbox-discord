package cmd

import (
	"datbox/comm"
	"log"

	"github.com/spf13/cobra"
)

var (
	long    = false
	human   = false
	listCmd = &cobra.Command{
		Use:   "ls",
		Short: "List files in a directory",
		Run: func(cmd *cobra.Command, args []string) {
			client, id, err := comm.StartClientAndWait()
			if err != nil {
				log.Fatalln(err)
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
				log.Fatalln(err)
			}
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
					log.Println("\n" + str)
					client.Close()
					break
				}
			}
		},
	}
)

func init() {
	listCmd.Flags().BoolVarP(&long, "long", "l", false, "Use a long listing format")
	listCmd.Flags().BoolVarP(&human, "human", "H", false, "Human readable file size")
}
