package cmd

import (
	"datbox/util"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

const (
	UploadHeaderChunk = 0
	UploadHeaderEnd   = 1
	UploadHeaderAbort = 2
	SendChunkSize     = 1024 * 1024 * 5
)

var (
	uploadFileVersion byte
	uploadCmd         = &cobra.Command{
		Use:   "upload <physical-path> <virtual-path>",
		Short: "Upload a file to the virtual file system",
		Args:  cobra.MinimumNArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			client, err := util.NewClientWrapperAndConnect()
			if err != nil {
				log.Fatalln(err)
			}
			defer client.Close()
			absPhys, err := filepath.Abs(args[0])
			if err != nil {
				log.Fatalln(err)
			}
			stat, err := os.Stat(absPhys)
			if err != nil {
				log.Fatalln(err)
			}
			// Establish reader
			writer := client.Prepare()
			writer.WriteByte(uploadFileVersion)
			writer.WriteUInt64(uint64(stat.Size()))
			writer.WriteUtf8(filepath.Base(absPhys))
			writer.WriteUtf8(args[1])
			err = client.Send(MSG_TYPE_UPLOAD)
			if err != nil {
				log.Printf("Failed to write data to ipc: %v", err)
			}
			err = client.ReadAllMsgs()
			if err != nil {
				log.Fatalln(err)
			}
			// Start upload
			file, err := os.Open(absPhys)
			if err != nil {
				writer = client.Prepare()
				writer.WriteByte(UploadHeaderAbort)
				client.Send(MSG_TYPE_UPLOAD_CHUNK)
				log.Fatalln(err)
			}
			data := make([]byte, SendChunkSize)
			for {
				writer = client.Prepare()
				read, err := file.Read(data)
				if err != nil {
					if err == io.EOF {
						writer.WriteByte(UploadHeaderEnd)
						writer.WriteUInt64(uint64(read))
						writer.WriteBytes(data[:read])
						client.Send(MSG_TYPE_UPLOAD_CHUNK)
						client.ReadAllMsgs()
						break
					}
					writer.WriteByte(UploadHeaderAbort)
					client.Send(MSG_TYPE_UPLOAD_CHUNK)
					log.Fatalln(err)
				}
				writer.WriteByte(UploadHeaderChunk)
				writer.WriteUInt64(uint64(read))
				writer.WriteBytes(data[:read])
				err = client.Send(MSG_TYPE_UPLOAD_CHUNK)
				client.ReadAllMsgs()
			}
		},
	}
)

func init() {
	uploadCmd.Flags().Uint8VarP(&uploadFileVersion, "file-version", "v", 1, "Use a specific file version for uploading")
}
