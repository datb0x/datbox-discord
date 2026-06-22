package cmd

import (
	"datbox/comm"
	"log"
	"os"
	"path"

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
			defer client.Close()
			stat, err := os.Stat(args[1])
			if err == nil {
				if !stat.IsDir() {
					log.Fatalf("Physical path %s already exists", args[1])
				} else {
					args[1] = path.Join(args[1], path.Base(args[0]))
					_, err := os.Stat(args[1])
					if err == nil {
						log.Fatalf("Physical path %s already exists", args[1])
					}
				}
			}
			file, err := os.Create(args[1])
			if err != nil {
				log.Fatalf("Failed to open file with create and write flag: %v", err)
			}
			defer file.Close()
			writer := comm.NewWriter()
			writer.WriteInt32(id)
			writer.WriteUtf8(args[0])
			err = client.Write(MSG_TYPE_DOWNLOAD, writer.Data)
			if err != nil {
				log.Fatalf("Failed to write data to ipc: %v", err)
			}
			err = comm.ReadUntilEndWithRaw(client, id, file)
			if err != nil {
				log.Fatalln(err)
			}
		},
	}
)
