package cmd

import (
	"datbox/comm"
	"datbox/network"
	"datbox/server"
	"encoding/hex"
	"fmt"
	"log"
	"path"
	"regexp"
	"strings"
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
		Run: func(cmd *cobra.Command, args []string) {
			config := server.NewConfig(configPath, channelId, dataDir, concurrency, token)
			if err := config.Load(); err != nil {
				log.Fatalln(err)
			}
			if err := config.Save(); err != nil {
				log.Fatalln(err)
			}
			network, err := network.NewNetwork(config.Raw.Token, config.Raw.ChannelId)
			if err != nil {
				log.Fatalln(err)
			}
			fs, err := server.NewFileSystem(config.Raw.DataDir, config.Raw.Concurrency, network)
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
	serverCmd.Flags().IntVarP(&concurrency, "concurrency", "m", 10, "Maximum number upload and download jobs that can run in parallel")
	serverCmd.Flags().StringVarP(&dataDir, "data-dir", "d", configDir, "Directory where data should be stored")
	serverCmd.Flags().StringVarP(&token, "token", "t", "", "Discord bot token. This option not recommended. Use config instead")
}

func handleMessage(server *ipc.Server, message *ipc.Message, fs *server.DatboxFileSystem) {
	if message.Err != nil {
		log.Println(message.Err)
		return
	}
	if message.MsgType <= 0 || message.MsgType > 7 {
		log.Printf("Unknown message type %d\n", message.MsgType)
		return
	}
	reader := comm.NewReader(message)
	id, err := reader.ReadInt32()
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
			sending := false
			result, err := fs.Upload(physicalPath, virtualPath, func(progress float32, must bool) {
				if sending && !must {
					return
				}
				sending = true
				writer.Clear()
				writer.WriteByte(2)
				writer.WriteFloat32(progress)
				server.Write(int(id), writer.Data)
				sending = false
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
			sending := false
			err = fs.Download(virtualPath, physicalPath, func(progress float32, must bool) {
				if sending && !must {
					return
				}
				sending = true
				writer.Clear()
				writer.WriteByte(2)
				writer.WriteFloat32(progress)
				server.Write(int(id), writer.Data)
				sending = false
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
			var body strings.Builder
			if long == 1 {
				months := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
				sizes := make([]string, len(entries))
				pad := 0
				re := regexp.MustCompile(`( \w)?B`)
				for ii, entry := range entries {
					var size string
					if human == 1 {
						size = units.HumanSize(float64(entry.Stat.Size()))
						size = strings.ToUpper(re.ReplaceAllString(size, ""))
						if len(size) > 4 {
							size = size[0:2] + string(size[len(size)-1])
						}
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
						body.WriteString(" " + entry.Name + "/\t")
					} else {
						body.WriteString(" " + entry.Name + "\t")
					}
				}
			}
			writer.Clear()
			writer.WriteByte(0)
			writer.WriteUtf8(body.String())
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
