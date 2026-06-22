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
	"path"
	"strings"
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
)

type Uploader struct {
	writer    *io.PipeWriter
	messenger chan string
}

var uploaders = map[int32]*Uploader{}

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
			server, err := ipc.StartServer("datbox", nil)
			if err != nil {
				log.Fatalln(err)
			}
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

func consumeMessenger(messenger chan string) (message string) {
	for {
		select {
		case message = <-messenger:
		default:
			return
		}
	}
}

func handleMessage(server *ipc.Server, message *ipc.Message, fs *server.DatboxFileSystem) {
	if message.Err != nil {
		log.Println(message.Err)
		return
	}
	if message.MsgType <= 0 || message.MsgType > 8 {
		log.Printf("Unknown message type %d\n", message.MsgType)
		return
	}
	reader := comm.NewReader(message)
	id, err := reader.ReadInt32()
	if err != nil {
		return
	}
	logger := comm.NewLogger(server, int(id))
	switch message.MsgType {
	case MSG_TYPE_UPLOAD:
		{
			fileVersion, err := reader.ReadByte()
			if err != nil {
				logger.SendFailure(err.Error())
				return
			}
			fileSize, err := reader.ReadUInt64()
			if err != nil {
				logger.SendFailure(err.Error())
				return
			}
			fileName, err := reader.ReadUtf8()
			if err != nil {
				logger.SendFailure(err.Error())
				return
			}
			virtualPath, err := reader.ReadUtf8()
			if err != nil {
				logger.SendFailure(err.Error())
				return
			}
			reader, writer := io.Pipe()
			messenger := make(chan string)
			uploaders[id] = &Uploader{
				writer:    writer,
				messenger: messenger,
			}
			logger.SendSuccess("")
			// Put reader in goroutine to upload
			go func() {
				log.Printf("(%d) Starting upload to %s\n", id, virtualPath)
				result, err := fs.Upload(reader, int64(fileSize), fileName, virtualPath, fileVersion, messenger)
				if err != nil {
					log.Printf("(%d) Failed upload to %s: %v", id, virtualPath, err)
					logger.SendFailure(err.Error())
					fs.Remove(virtualPath, false, true)
				} else {
					log.Printf("(%d) Finished upload to %s", id, virtualPath)
					logger.SendIntermediate(consumeMessenger(messenger))
					logger.SendIntermediate(fmt.Sprintf("\nUploaded to %s as %d chunks (MD5 %s)", path.Join("/", virtualPath), result.Chunks, hex.EncodeToString(result.Checksum)))
					logger.SendSuccess(fmt.Sprintf("\nTime elapsed: %s", humanize.RelTime(result.StartTime, result.EndTime, "", "")))
				}
				delete(uploaders, id)
			}()
			break
		}
	case MSG_TYPE_UPLOAD_CHUNK:
		{
			uploader := uploaders[id]
			if uploader != nil {
				header, err := reader.ReadByte()
				if err != nil {
					logger.SendFailure(err.Error())
					break
				}
				switch header {
				case UploadHeaderChunk, UploadHeaderEnd:
					size, err := reader.ReadUInt64()
					if err != nil {
						logger.SendFailure(err.Error())
						break
					}
					data, err := reader.ReadNBytes(int(size))
					if err != nil {
						logger.SendFailure(err.Error())
						break
					}
					uploader.writer.Write(data)
					if header == UploadHeaderEnd {
						uploader.writer.Close()
						// The main goroutine will send success
					} else {
						logger.SendSuccess(consumeMessenger(uploader.messenger))
					}
				case UploadHeaderAbort:
					uploader.writer.CloseWithError(errors.New("Client aborted"))
					logger.SendSuccess("")
				}
			} else {
				logger.SendFailure("No writer")
			}
			break
		}
	case MSG_TYPE_DOWNLOAD:
		{
			virtualPath, err := reader.ReadUtf8()
			if err != nil {
				logger.SendFailure(err.Error())
				return
			}
			physicalPath, err := reader.ReadUtf8()
			if err != nil {
				logger.SendFailure(err.Error())
				return
			}
			// Logger automatically succeeds if no error
			result, err := fs.Download(virtualPath, physicalPath, logger)
			if err != nil {
				logger.SendFailure(err.Error())
			} else {
				logger.SendIntermediate(fmt.Sprintf("\nDownloaded to %s successfully", physicalPath))
				logger.SendSuccess(fmt.Sprintf("\nTime elapsed: %s", humanize.RelTime(result.StartTime, time.Now(), "", "")))
			}
			break
		}
	case MSG_TYPE_LIST:
		{
			long, err := reader.ReadByte()
			if err != nil {
				logger.SendFailure(err.Error())
				return
			}
			human, err := reader.ReadByte()
			if err != nil {
				logger.SendFailure(err.Error())
				return
			}
			virtualPath, err := reader.ReadUtf8()
			if err != nil || virtualPath == "" {
				virtualPath = "/"
			}
			entries, err := fs.ReadDir(virtualPath, long == 1)
			if err != nil {
				logger.SendFailure(err.Error())
				return
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
			logger.SendSuccess(body.String())
			break
		}
	case MSG_TYPE_MOVE:
		{
			src, err := reader.ReadUtf8()
			if err != nil {
				logger.SendFailure(err.Error())
				return
			}
			dest, err := reader.ReadUtf8()
			if err != nil {
				logger.SendFailure(err.Error())
				return
			}
			err = fs.Move(src, dest)
			if err != nil {
				logger.SendFailure(err.Error())
			} else {
				logger.SendSuccess("")
			}
			break
		}
	case MSG_TYPE_REMOVE:
		{
			virtualPath, err := reader.ReadUtf8()
			if err != nil {
				logger.SendFailure(err.Error())
				return
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
				logger.SendFailure(err.Error())
			} else {
				logger.SendSuccess("")
			}
			break
		}
	case MSG_TYPE_MKDIR:
		{
			virtualPath, err := reader.ReadUtf8()
			if err != nil {
				logger.SendFailure(err.Error())
				return
			}
			recursive, err := reader.ReadByte()
			if err != nil {
				recursive = 0
			}
			err = fs.Mkdir(virtualPath, recursive == 1)
			if err != nil {
				logger.SendFailure(err.Error())
			} else {
				logger.SendSuccess("")
			}
			break
		}
	case MSG_TYPE_COPY:
		{
			src, err := reader.ReadUtf8()
			if err != nil {
				logger.SendFailure(err.Error())
				return
			}
			dest, err := reader.ReadUtf8()
			if err != nil {
				logger.SendFailure(err.Error())
				return
			}
			err = fs.Copy(src, dest)
			if err != nil {
				logger.SendFailure(err.Error())
			} else {
				logger.SendSuccess("")
			}
			break
		}
	}
}

func createLogger(server *ipc.Server, id int) chan []byte {
	logger := make(chan []byte)
	go func() {
		sending := false
		for {
			message := <-logger
			if message[0] == 0 || message[0] == 1 {
				for sending {
					time.Sleep(100 * time.Millisecond)
				}
				server.Write(id, message)
			} else {
				if sending && message[0] != 2 {
					continue
				}
				sending = true
				go func() {
					server.Write(id, message)
					sending = false
				}()
			}
		}
	}()
	return logger
}
