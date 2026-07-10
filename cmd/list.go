package cmd

import (
	"datbox/util"
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
			client, err := util.NewClientWrapperAndConnect()
			if err != nil {
				log.Fatalln(err)
			}
			defer client.Close()
			writer := client.Prepare()
			writer.WriterBool(long)
			writer.WriterBool(human)
			if len(args) > 0 {
				writer.WriteUtf8(args[0])
			}
			err = client.Send(MSG_TYPE_LIST)
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

func init() {
	listCmd.Flags().BoolVarP(&long, "long", "l", false, "Use a long listing format")
	listCmd.Flags().BoolVarP(&human, "human", "H", false, "Human readable file size")
}
