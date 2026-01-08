package cmd

import (
	"datbox/comm"
	"datbox/server"
	"encoding/hex"
	"fmt"
	"log"
	"regexp"
	"time"

	"github.com/adrg/xdg"
	"github.com/docker/go-units"
	ipc "github.com/james-barrow/golang-ipc"
	"github.com/spf13/cobra"
)

const (
	MSG_TYPE_UPLOAD   = 1
	MSG_TYPE_DOWNLOAD = 2
	MSG_TYPE_LIST     = 3
	MSG_TYPE_MOVE     = 4
	MSG_TYPE_REMOVE   = 5
	MSG_TYPE_MKDIR    = 6
	MSG_TYPE_COPY     = 7
)

var (
	channelId   string
	configPath  string
	concurrency int
	dataDir     string
	token       string
	serverCmd   = &cobra.Command{
		Use:   "server",
		Short: "Run the datbox local server",
		RunE: func(cmd *cobra.Command, args []string) error {
			config := server.NewConfig(configPath, channelId, dataDir, concurrency, token)
			if err := config.Load(); err != nil {
				return err
			}
			if err := config.Save(); err != nil {
				return err
			}
			network, err := server.NewNetwork(config.Raw.Token, config.Raw.ChannelId)
			if err != nil {
				return err
			}
			fs, err := server.NewFileSystem(config.Raw.DataDir, config.Raw.Concurrency, network)
			if err != nil {
				return err
			}
			server, err := ipc.StartServer("datbox", nil)
			if err != nil {
				return err
			}
			for {
				message, err := server.Read()
				if err != nil {
					return err
				}
				go handleMessage(server, message, fs)
			}
		},
	}
)

func init() {
	configFilePath, err := xdg.ConfigFile("datbox/config.json")
	if err != nil {
		log.Fatalf("Error getting config path: %v", err)
	}

	serverCmd.PersistentFlags().StringVarP(&channelId, "channel", "c", "", "ID of the text channel where chunks will be stored")
	serverCmd.PersistentFlags().StringVarP(&configPath, "config", "C", configFilePath, "Local path to config file")
	serverCmd.PersistentFlags().IntVarP(&concurrency, "concurrency", "m", 10, "Maximum number upload and download jobs that can run in parallel")
	serverCmd.PersistentFlags().StringVarP(&dataDir, "data-dir", "d", "", "Directory where data should be stored")
	serverCmd.PersistentFlags().StringVarP(&token, "token", "t", "", "Discord bot token. This option not recommended. Use .env or config instead")
}

func handleMessage(server *ipc.Server, message *ipc.Message, fs *server.DatboxFileSystem) {
	if message.MsgType <= 0 || message.MsgType > 7 {
		log.Printf("Unknown message type %d\n", message.MsgType)
		return
	}
	reader := comm.NewReader(message)
	id, err := reader.ReadUInt32()
	if err != nil {
		return
	}
	writer := comm.NewWriter()
	switch message.MsgType {
	case MSG_TYPE_UPLOAD:
		{
			physicalPath, err := reader.ReadUtf8()
			if err != nil {
				writer.WriteByte(1)
				writer.WriteUtf8(fmt.Sprint(err))
				server.Write(int(id), writer.Data)
				return
			}
			virtualPath, err := reader.ReadUtf8()
			if err != nil {
				writer.WriteByte(1)
				writer.WriteUtf8(fmt.Sprint(err))
				server.Write(int(id), writer.Data)
				return
			}
			result, err := fs.Upload(physicalPath, virtualPath, func(current, total int64) {
				writer.Clear()
				writer.WriteByte(2)
				writer.WriteUInt64(uint64(current))
				writer.WriteUInt64(uint64(total))
				server.Write(int(id), writer.Data)
			})
			writer.Clear()
			if err == nil {
				writer.WriteByte(0)
				writer.WriteUtf8(fmt.Sprintf("Uploaded to %s as %d chunks (MD5 %s)", result.Path, result.Chunks, hex.EncodeToString(result.Checksum)))
				server.Write(int(id), writer.Data)
			} else {
				writer.WriteByte(1)
				writer.WriteUtf8(fmt.Sprint(err))
				server.Write(int(id), writer.Data)
			}
			break
		}
	case MSG_TYPE_DOWNLOAD:
		{
			virtualPath, err := reader.ReadUtf8()
			if err != nil {
				writer.WriteByte(1)
				writer.WriteUtf8(fmt.Sprint(err))
				server.Write(int(id), writer.Data)
				return
			}
			physicalPath, err := reader.ReadUtf8()
			if err != nil {
				writer.WriteByte(1)
				writer.WriteUtf8(fmt.Sprint(err))
				server.Write(int(id), writer.Data)
				return
			}
			err = fs.Download(virtualPath, physicalPath, func(current, total int64) {
				writer.Clear()
				writer.WriteByte(2)
				writer.WriteUInt64(uint64(current))
				writer.WriteUInt64(uint64(total))
				server.Write(int(id), writer.Data)
			})
			writer.Clear()
			if err == nil {
				writer.WriteByte(0)
				writer.WriteUtf8(fmt.Sprintf("Downloaded to %s successfully", physicalPath))
				server.Write(int(id), writer.Data)
			} else {
				writer.WriteByte(1)
				writer.WriteUtf8(fmt.Sprint(err))
				server.Write(int(id), writer.Data)
			}
			break
		}
	case MSG_TYPE_LIST:
		{
			long, err := reader.ReadByte()
			if err != nil {
				writer.WriteByte(1)
				writer.WriteUtf8(fmt.Sprint(err))
				server.Write(int(id), writer.Data)
				return
			}
			human, err := reader.ReadByte()
			if err != nil {
				writer.WriteByte(1)
				writer.WriteUtf8(fmt.Sprint(err))
				server.Write(int(id), writer.Data)
				return
			}
			virtualPath, err := reader.ReadUtf8()
			if err != nil || virtualPath == "" {
				virtualPath = "/"
			}
			entries, err := fs.ReadDir(virtualPath, long == 1)
			if err != nil {
				writer.WriteByte(1)
				writer.WriteUtf8(fmt.Sprint(err))
				server.Write(int(id), writer.Data)
				return
			}
			body := ""
			if long == 1 {
				months := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
				sizes := make([]string, len(entries))
				pad := 0
				re := regexp.MustCompile(` \wB`)
				for _, entry := range entries {
					var size string
					if human == 1 {
						size = units.HumanSizeWithPrecision(float64(entry.Stat.Size()), 1)
						size = re.ReplaceAllString(size, "")
					} else {
						size = fmt.Sprint(entry.Stat.Size())
					}
					pad = max(pad, len(size))
					sizes = append(sizes, size)
				}
				for ii, entry := range entries {
					if ii != 0 {
						body += "\n"
					}
					body += fmt.Sprintf(fmt.Sprintf("%%%d0s", pad), sizes[ii])
					date := entry.Stat.ModTime()
					body += " " + months[date.Month()-1]
					body += fmt.Sprintf(" %02d", date.Day())
					if date.Year() < time.Now().Year() {
						body += fmt.Sprintf("  %d", date.Year())
					} else {
						body += fmt.Sprintf(" %02d:%02d", date.Hour(), date.Minute())
					}
					if entry.Stat.IsDir() {
						body += " " + entry.Name + "/"
					} else {
						body += " " + entry.Name
					}
				}
			} else {
				for _, entry := range entries {
					if entry.Stat.IsDir() {
						body += " " + entry.Name + "/\t"
					} else {
						body += " " + entry.Name + "\t"
					}
				}
			}
			writer.Clear()
			writer.WriteByte(0)
			writer.WriteUtf8(body)
			server.Write(int(id), writer.Data)
			break
		}
	case MSG_TYPE_MOVE:
		{
			src, err := reader.ReadUtf8()
			if err != nil {
				writer.WriteByte(1)
				writer.WriteUtf8(fmt.Sprint(err))
				server.Write(int(id), writer.Data)
				return
			}
			dest, err := reader.ReadUtf8()
			if err != nil {
				writer.WriteByte(1)
				writer.WriteUtf8(fmt.Sprint(err))
				server.Write(int(id), writer.Data)
				return
			}
			err = fs.Move(src, dest)
			if err != nil {
				writer.WriteByte(1)
				writer.WriteUtf8(fmt.Sprint(err))
			} else {
				writer.WriteByte(0)
			}
			server.Write(int(id), writer.Data)
			break
		}
	case MSG_TYPE_REMOVE:
		{
			virtualPath, err := reader.ReadUtf8()
			if err != nil {
				writer.WriteByte(1)
				writer.WriteUtf8(fmt.Sprint(err))
				server.Write(int(id), writer.Data)
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
				writer.WriteByte(1)
				writer.WriteUtf8(fmt.Sprint(err))
			} else {
				writer.WriteByte(0)
			}
			server.Write(int(id), writer.Data)
			break
		}
	case MSG_TYPE_MKDIR:
		{
			virtualPath, err := reader.ReadUtf8()
			if err != nil {
				writer.WriteByte(1)
				writer.WriteUtf8(fmt.Sprint(err))
				server.Write(int(id), writer.Data)
				return
			}
			recursive, err := reader.ReadByte()
			if err != nil {
				recursive = 0
			}
			err = fs.Mkdir(virtualPath, recursive == 1)
			if err != nil {
				writer.WriteByte(1)
				writer.WriteUtf8(fmt.Sprint(err))
			} else {
				writer.WriteByte(0)
			}
			server.Write(int(id), writer.Data)
			break
		}
	case MSG_TYPE_COPY:
		{
			src, err := reader.ReadUtf8()
			if err != nil {
				writer.WriteByte(1)
				writer.WriteUtf8(fmt.Sprint(err))
				server.Write(int(id), writer.Data)
				return
			}
			dest, err := reader.ReadUtf8()
			if err != nil {
				writer.WriteByte(1)
				writer.WriteUtf8(fmt.Sprint(err))
				server.Write(int(id), writer.Data)
				return
			}
			err = fs.Copy(src, dest)
			if err != nil {
				writer.WriteByte(1)
				writer.WriteUtf8(fmt.Sprint(err))
			} else {
				writer.WriteByte(0)
			}
			server.Write(int(id), writer.Data)
			break
		}
	}
}
