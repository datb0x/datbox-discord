package cmd

import (
	"datbox/comm"
	"log"
	"path/filepath"

	"github.com/spf13/cobra"
)

var (
	uploadFileVersion byte
	uploadCmd         = &cobra.Command{
		Use:   "upload <physical-path> <virtual-path>",
		Short: "Upload a file to the virtual file system",
		Args:  cobra.MinimumNArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			client, id, err := comm.StartClientAndWait()
			if err != nil {
				log.Fatalln(err)
			}
			absPhys, err := filepath.Abs(args[0])
			if err != nil {
				log.Fatalln(err)
			}
			writer := comm.NewWriter()
			writer.WriteInt32(id)
			writer.WriteByte(uploadFileVersion)
			writer.WriteUtf8(absPhys)
			writer.WriteUtf8(args[1])
			err = client.Write(MSG_TYPE_UPLOAD, writer.Data)
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

func init() {
	uploadCmd.Flags().Uint8VarP(&uploadFileVersion, "file-version", "v", 1, "Use a specific file version for uploading")
}
