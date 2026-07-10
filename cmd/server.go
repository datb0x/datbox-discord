package cmd

import (
	"datbox/comm"
	"datbox/network"
	"datbox/server"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/adrg/xdg"
	"github.com/dustin/go-humanize"
	ipc "github.com/james-barrow/golang-ipc"
	"github.com/spf13/cobra"
)

const (
	MSG_TYPE_UPLOAD       = 1
	MSG_TYPE_UPLOAD_CHUNK = 8
	MSG_TYPE_DOWNLOAD     = 2
	MSG_TYPE_LIST         = 3
	MSG_TYPE_MOVE         = 4
	MSG_TYPE_REMOVE       = 5
	MSG_TYPE_MKDIR        = 6
	MSG_TYPE_COPY         = 7
	MSG_TYPE_INFO         = 9
)

type Uploader struct {
	writer  *io.PipeWriter
	message *string
}

var (
	uploaders = map[int]*Uploader{}
	startTime time.Time
)

var (
	channelId   string
	configPath  string
	concurrency int
	dataDir     string
	encrypted   bool
	token       string
	serverCmd   = &cobra.Command{
		Use:   "server",
		Short: "Run the datbox local server",
		Run: func(cmd *cobra.Command, args []string) {
			config := server.NewConfig(configPath, channelId, dataDir, concurrency, encrypted, token)
			if err := config.Load(); err != nil {
				log.Fatalln(err)
			}
			if err := config.Save(); err != nil {
				log.Fatalln(err)
			}
			network, err := network.NewNetwork(config.Raw.Token, config.Raw.ChannelId, config.Raw.Concurrency)
			if err != nil {
				log.Fatalln(err)
			}
			fs, err := server.NewFileSystem(config, network)
			if err != nil {
				log.Fatalln(err)
			}
			server, err := ipc.StartServer("datbox", &ipc.ServerConfig{MaxMsgSize: 1024 * 1024 * 6})
			if err != nil {
				log.Fatalln(err)
			}
			startTime = time.Now()
			for {
				message, err := server.Read()
				if err != nil {
					if err.Error() != "Error: not enough data to decrypt" {
						continue
					}
					log.Fatalln(err)
				}
				if message.MsgType == -1 {
					if message.Status == "Connected" {
						server.Write(1, []byte{})
					}
				} else {
					go handleMessage(server, message, fs)
				}
			}
		},
	}
)

func init() {
	configDir, err := xdg.ConfigFile("datbox-go")
	if err != nil {
		log.Fatalf("Error getting config path: %v", err)
	}
	configFilePath := path.Join(configDir, "config.json")

	serverCmd.Flags().StringVarP(&channelId, "channel", "c", "", "ID of the text channel where chunks will be stored")
	serverCmd.Flags().StringVarP(&configPath, "config", "C", configFilePath, "Local path to config file")
	serverCmd.Flags().IntVarP(&concurrency, "concurrency", "m", 5, "Maximum number upload and download jobs that can run in parallel")
	serverCmd.Flags().StringVarP(&dataDir, "data-dir", "d", configDir, "Directory where data should be stored")
	serverCmd.Flags().BoolVarP(&encrypted, "encrypted", "e", false, "Use an extra password to encrypt the entire file system on Discord")
	serverCmd.Flags().StringVarP(&token, "token", "t", "", "Discord bot token. This option not recommended. Use config instead")
}

func handleMessage(server *ipc.Server, message *ipc.Message, fs *server.DatboxFileSystem) {
	if message.Err != nil {
		log.Println(message.Err)
		return
	}
	reader := comm.NewDataWrapper(message.Data)
	id, err := reader.ReadInt32()
	if err != nil {
		return
	}
	wrapper := comm.NewServerWrapper(server, int(id))
	err = runAction(message.MsgType, reader, wrapper, fs)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}

func runAction(msgType int, reader *comm.DataWrapper, server *comm.IPCServer, fs *server.DatboxFileSystem) error {
	switch msgType {
	case MSG_TYPE_UPLOAD:
		{
			fileVersion, err1 := reader.ReadByte()
			fileSize, err2 := reader.ReadUInt64()
			fileName, err3 := reader.ReadUtf8()
			virtualPath, err4 := reader.ReadUtf8()
			err := errors.Join(err1, err2, err3, err4)
			if err != nil {
				server.SendFailure(err.Error())
				return err
			}
			reader, writer := io.Pipe()
			var message string
			uploaders[server.ID] = &Uploader{
				writer:  writer,
				message: &message,
			}
			server.SendSuccess("", true)
			server.Reset()
			// Put reader in goroutine to upload
			go func() {
				log.Printf("(%d) Starting upload to %s\n", server.ID, virtualPath)
				result, err := fs.Upload(reader, int64(fileSize), fileName, virtualPath, fileVersion, &message)
				if err != nil {
					log.Printf("(%d) Failed upload to %s: %v", server.ID, virtualPath, err)
					server.SendFailure(err.Error())
				} else {
					log.Printf("(%d) Finished upload to %s", server.ID, virtualPath)
					server.SendIntermediate(message, true)
					server.SendIntermediate(fmt.Sprintf("\nUploaded to %s as %d chunks (MD5 %s)", path.Join("/", virtualPath), result.Chunks, hex.EncodeToString(result.Checksum)), true)
					server.SendSuccess(fmt.Sprintf("\nTime elapsed: %s", humanize.RelTime(result.StartTime, result.EndTime, "", "")))
				}
				delete(uploaders, server.ID)
			}()
			break
		}
	case MSG_TYPE_UPLOAD_CHUNK:
		{
			uploader := uploaders[server.ID]
			if uploader != nil {
				header, err := reader.ReadByte()
				if err != nil {
					server.SendFailure(err.Error())
					return err
				}
				switch header {
				case UploadHeaderChunk, UploadHeaderEnd:
					size, err1 := reader.ReadUInt64()
					data, err2 := reader.ReadNBytes(int(size))
					err = errors.Join(err1, err2)
					if err != nil {
						server.SendFailure(err.Error())
						return err
					}
					uploader.writer.Write(data)
					if header == UploadHeaderEnd {
						uploader.writer.Close()
						// The main goroutine will send success
					} else {
						server.SendSuccess(*uploader.message)
					}
				case UploadHeaderAbort:
					uploader.writer.CloseWithError(errors.New("Client aborted"))
				}
			} else {
				server.SendFailure("No writer")
			}
			break
		}
	case MSG_TYPE_DOWNLOAD:
		{
			virtualPath, err := reader.ReadUtf8()
			if err != nil {
				server.SendFailure(err.Error())
				return err
			}
			reader, writer := io.Pipe()
			mutex := sync.Mutex{}
			mutex.Lock()
			defer mutex.Unlock()
			var message string
			go func() {
				log.Printf("(%d) Starting download of %s\n", server.ID, virtualPath)
				result, err := fs.Download(writer, virtualPath, &message)
				mutex.Lock()
				defer mutex.Unlock()
				if err != nil {
					log.Printf("(%d) Failed download of %s: %v", server.ID, virtualPath, err)
					server.SendFailure(err.Error())
				} else {
					log.Printf("(%d) Finished download of %s", server.ID, virtualPath)
					server.SendIntermediate(message, true)
					server.SendIntermediate(fmt.Sprintf("\nDownloaded %s successfully", virtualPath), true)
					server.SendSuccess(fmt.Sprintf("\nTime elapsed: %s", humanize.RelTime(result.StartTime, time.Now(), "", "")))
				}
			}()
			data := make([]byte, 1024*1024*5)
			for {
				read, err := reader.Read(data)
				if err != nil {
					// The goroutine above this will handle the error
					if err == io.EOF {
						break
					}
					return err
				}
				server.SendRaw(data[:read], true)
				server.SendIntermediate(message)
			}
			break
		}
	case MSG_TYPE_LIST:
		{
			long, err1 := reader.ReadByte()
			human, err2 := reader.ReadByte()
			err := errors.Join(err1, err2)
			if err != nil {
				server.SendFailure(err.Error())
				return err
			}
			virtualPath, err := reader.ReadUtf8()
			if err != nil || virtualPath == "" {
				virtualPath = "/"
			}
			entries, err := fs.ReadDir(virtualPath, long == 1)
			if err != nil {
				server.SendFailure(err.Error())
				return err
			}
			var body strings.Builder
			if long == 1 {
				months := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
				sizes := make([]string, len(entries))
				pad := 0
				for ii, entry := range entries {
					var size string
					if human == 1 {
						size = humanize.Bytes(uint64(entry.Stat.Size()))
						size = strings.Replace(size, " ", "", 1)
						size = strings.Replace(size, "B", "", 1)
					} else {
						size = fmt.Sprint(entry.Stat.Size())
					}
					pad = max(pad, len(size))
					sizes[ii] = size
				}
				for ii, entry := range entries {
					if ii != 0 {
						body.WriteString("\n")
					}
					fmt.Fprintf(&body, fmt.Sprintf("%%%ds", pad), sizes[ii])
					date := entry.Stat.ModTime()
					body.WriteString(" " + months[date.Month()-1])
					fmt.Fprintf(&body, " %02d", date.Day())
					if date.Year() < time.Now().Year() {
						fmt.Fprintf(&body, "  %d", date.Year())
					} else {
						fmt.Fprintf(&body, " %02d:%02d", date.Hour(), date.Minute())
					}
					if entry.Stat.IsDir() {
						body.WriteString(" " + entry.Name + "/")
					} else {
						body.WriteString(" " + entry.Name)
					}
				}
			} else {
				for _, entry := range entries {
					if entry.Stat.IsDir() {
						body.WriteString(entry.Name + "/\t")
					} else {
						body.WriteString(entry.Name + "\t")
					}
				}
			}
			server.SendSuccess(body.String())
			break
		}
	case MSG_TYPE_MOVE:
		{
			src, err1 := reader.ReadUtf8()
			dest, err2 := reader.ReadUtf8()
			err := errors.Join(err1, err2)
			if err != nil {
				server.SendFailure(err.Error())
				return err
			}
			err = fs.Move(src, dest)
			if err != nil {
				server.SendFailure(err.Error())
			} else {
				server.SendSuccess("")
			}
			break
		}
	case MSG_TYPE_REMOVE:
		{
			virtualPath, err := reader.ReadUtf8()
			if err != nil {
				server.SendFailure(err.Error())
				return err
			}
			recursive, err := reader.ReadByte()
			if err != nil {
				recursive = 0
			}
			remote, err := reader.ReadByte()
			if err != nil {
				remote = 0
			}
			err = fs.Remove(virtualPath, recursive == 1, remote == 1)
			if err != nil {
				server.SendFailure(err.Error())
			} else {
				server.SendSuccess("")
			}
			break
		}
	case MSG_TYPE_MKDIR:
		{
			virtualPath, err := reader.ReadUtf8()
			if err != nil {
				server.SendFailure(err.Error())
				return err
			}
			recursive, err := reader.ReadByte()
			if err != nil {
				recursive = 0
			}
			err = fs.Mkdir(virtualPath, recursive == 1)
			if err != nil {
				server.SendFailure(err.Error())
			} else {
				server.SendSuccess("")
			}
			break
		}
	case MSG_TYPE_COPY:
		{
			src, err1 := reader.ReadUtf8()
			dest, err2 := reader.ReadUtf8()
			err := errors.Join(err1, err2)
			if err != nil {
				server.SendFailure(err.Error())
				return err
			}
			err = fs.Copy(src, dest)
			if err != nil {
				server.SendFailure(err.Error())
			} else {
				server.SendSuccess("")
			}
			break
		}
	case MSG_TYPE_INFO:
		{
			server.SendIntermediate(fmt.Sprintf("Start time: %s", startTime.Format("2006-01-02 15:04:05")), true)
			server.SendIntermediate(fmt.Sprintf("Uptime: %s", humanize.RelTime(startTime, time.Now(), "", "")), true)
			server.SendSuccess(fs.Info())
			break
		}
	default:
		log.Printf("Unknown message type %d\n", msgType)
		server.SendFailure(fmt.Sprintf("Unknown message type %d\n", msgType))
	}
	return nil
}
