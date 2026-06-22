package cmd

import (
	"datbox/comm"
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
			client, id, err := comm.StartClientAndWait()
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
			writer := comm.NewWriter()
			writer.WriteInt32(id)
			writer.WriteByte(uploadFileVersion)
			writer.WriteUInt64(uint64(stat.Size()))
			writer.WriteUtf8(filepath.Base(absPhys))
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
			// Start upload
			file, err := os.Open(absPhys)
			if err != nil {
				writer.Clear()
				writer.WriteInt32(id)
				writer.WriteByte(UploadHeaderAbort)
				client.Write(MSG_TYPE_UPLOAD_CHUNK, writer.Data)
				log.Fatalln(err)
			}
			data := make([]byte, SendChunkSize)
			for {
				writer.Clear()
				writer.WriteInt32(id)
				read, err := file.Read(data)
				if err != nil {
					if err == io.EOF {
						writer.WriteByte(UploadHeaderEnd)
						writer.WriteUInt64(uint64(read))
						writer.WriteBytes(data[:read])
						client.Write(MSG_TYPE_UPLOAD_CHUNK, writer.Data)
						comm.ReadUntilEnd(client, id)
						break
					}
					writer.WriteByte(UploadHeaderAbort)
					client.Write(MSG_TYPE_UPLOAD_CHUNK, writer.Data)
					log.Fatalln(err)
				}
				writer.WriteByte(UploadHeaderChunk)
				writer.WriteUInt64(uint64(read))
				writer.WriteBytes(data[:read])
				client.Write(MSG_TYPE_UPLOAD_CHUNK, writer.Data)
				comm.ReadUntilEnd(client, id)
			}
		},
	}
)

func init() {
	uploadCmd.Flags().Uint8VarP(&uploadFileVersion, "file-version", "v", 1, "Use a specific file version for uploading")
}
