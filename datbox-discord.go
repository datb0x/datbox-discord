package datboxdiscord

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"syscall"
	"time"

	datboxcore "github.com/datb0x/datbox-core"
	"github.com/datb0x/datbox-discord/internal"
	"github.com/datb0x/datbox-discord/network"
	"github.com/dustin/go-humanize"
	"golang.org/x/term"
)

type discordConfig struct {
	ChannelId   string `json:"channelId"`
	DataDir     string `json:"dataDir"`
	Concurrency int    `json:"concurrency"`
	Password    string `json:"password"`
	Token       string `json:"token"`
}

type DiscordProvider struct {
	fs *discordFileSystem
}

func NewDiscordProvider(configPath string) (datboxcore.Provider, error) {
	// Load config
	var config discordConfig
	_, err := os.Stat(configPath)
	if err == nil {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return nil, err
		}
		err = json.Unmarshal(data, &config)
		if err != nil {
			return nil, err
		}
	}

	// Check for required fields
	reader := bufio.NewReader(os.Stdin)
	if config.Token == "" {
		internal.Logger.Print("Enter Discord bot token: ")
		data, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		config.Token = strings.TrimRight(data, "\n")
	}
	if config.ChannelId == "" {
		internal.Logger.Print("Enter Discord channel ID: ")
		data, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		config.ChannelId = strings.TrimRight(data, "\n")
	}
	if config.DataDir == "" {
		config.DataDir = path.Dir(configPath)
	}
	if config.Concurrency == 0 {
		config.Concurrency = 3
	}
	if config.Password == "" {
		internal.Logger.Print("Enter password for encryption (leave blank to disable): ")
		data, err := term.ReadPassword(syscall.Stdin)
		if err != nil {
			return nil, err
		}
		password := strings.TrimRight(string(data), "\n")
		internal.Logger.Println()
		if password == "" {
			config.Password = "skip"
		} else {
			hasher := sha256.New()
			hasher.Write([]byte(password))
			config.Password = hex.EncodeToString(hasher.Sum(nil))
		}
	}

	// Save config
	data, err := json.Marshal(config)
	if err != nil {
		internal.Logger.Errorf("Failed to stringify config: %v", err)
	}
	err = os.WriteFile(configPath, data, 0o755)
	if err != nil {
		internal.Logger.Errorf("Failed to save config: %v", err)
	}

	network, err := network.NewNetwork(config.Token, config.ChannelId, config.Concurrency)
	if err != nil {
		return nil, err
	}
	fs, err := newFileSystem(&config, network)
	if err != nil {
		return nil, err
	}
	provider := DiscordProvider{fs: fs}
	return &provider, nil
}

func (prov *DiscordProvider) Get(path string, writer io.WriteCloser) error {
	result, err := prov.fs.Download(writer, path)
	if err != nil {
		return err
	}
	internal.Logger.Infof("Downloaded %s successfully", path)
	internal.Logger.Infof("Time elapsed: %s", humanize.RelTime(result.StartTime, time.Now(), "", ""))
	return nil
}

func (prov *DiscordProvider) Put(path string, size int64, reader io.ReadCloser) error {
	result, err := prov.fs.Upload(reader, path, size, 1)
	if err != nil {
		return err
	}
	internal.Logger.Infof("Uploaded %s successfully", path)
	internal.Logger.Infof("Time elapsed: %s", humanize.RelTime(result.StartTime, time.Now(), "", ""))
	return nil
}

func (prov *DiscordProvider) List(path string) ([]fs.DirEntry, error) {
	return prov.fs.ReadDir(path, true)
}

func (prov *DiscordProvider) Move(src, dst string) error {
	return prov.fs.Move(src, dst)
}

func (prov *DiscordProvider) Delete(path string, recursive, remote bool) error {
	return prov.fs.Remove(path, recursive, remote)
}

func (prov *DiscordProvider) Mkdir(path string, parents bool) error {
	return prov.fs.Mkdir(path, 0o755, parents)
}

func (prov *DiscordProvider) Copy(src, dst string) error {
	return prov.fs.Copy(src, dst)
}

func (prov *DiscordProvider) Info() (datboxcore.ProviderInfo, error) {
	var totalFiles, totalBytes uint64
	var recurse func(virtualPath string)
	recurse = func(virtualPath string) {
		stat, err := prov.fs.Stat(virtualPath, false)
		if err != nil {
			return
		}
		if stat.IsDir() {
			entries, err := prov.fs.ReadDir(virtualPath)
			if err != nil {
				return
			}
			for _, entry := range entries {
				recurse(path.Join(virtualPath, entry.Name()))
			}
		} else {
			totalFiles++
			totalBytes += uint64(stat.Size())
		}
	}
	recurse("/")

	return datboxcore.ProviderInfo{
		Root:        prov.fs.root,
		Encrypted:   prov.fs.config.Password != "" && prov.fs.config.Password != "skip",
		StoredFiles: uint(totalFiles),
		StoredBytes: uint(totalBytes),
	}, nil
}
