package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"syscall"

	"golang.org/x/term"
)

type RawConfig struct {
	ChannelId   string `json:"channelId"`
	DataDir     string `json:"dataDir"`
	Concurrency int    `json:"concurrency"`
	Password    string `json:"password"`
	Token       string `json:"token"`
}

type DatboxConfig struct {
	configPath string
	Raw        RawConfig
	Encrypted  bool
}

func NewConfig(configPath string, channelId, dataDir string, concurrency int, encrypted bool, token string) *DatboxConfig {
	config := new(DatboxConfig)
	config.configPath = configPath
	config.Raw.ChannelId = channelId
	config.Raw.DataDir = dataDir
	config.Raw.Concurrency = concurrency
	config.Raw.Token = token
	config.Encrypted = encrypted
	return config
}

func (config *DatboxConfig) Load() error {
	if _, err := os.Stat(config.configPath); err == nil {
		// config exists
		file, err := os.Open(config.configPath)
		if err != nil {
			return err
		}
		defer file.Close()

		bytes, err := io.ReadAll(file)
		if err != nil {
			return err
		}
		rawConfig := new(RawConfig)
		json.Unmarshal(bytes, &rawConfig)
		if rawConfig.ChannelId != "" {
			config.Raw.ChannelId = rawConfig.ChannelId
		}
		if rawConfig.DataDir != "" {
			config.Raw.DataDir = rawConfig.DataDir
		}
		if rawConfig.Concurrency != 0 {
			config.Raw.Concurrency = rawConfig.Concurrency
		}
		if rawConfig.Password != "" {
			config.Raw.Password = rawConfig.Password
		}
		if rawConfig.Token != "" {
			config.Raw.Token = rawConfig.Token
		}
	} else {
		log.Print("Configuration not found. Default settings will be used, except for channelId")
	}
	if config.Raw.ChannelId == "" {
		return errors.New("Missing channelId")
	}
	if config.Raw.DataDir == "" {
		config.Raw.DataDir = path.Join(path.Dir(config.configPath), "root")
	}
	if config.Raw.Concurrency <= 0 {
		return errors.New("Invalid concurrency")
	}
	if config.Raw.Token == "" {
		return errors.New("Missing token")
	}
	if config.Encrypted && config.Raw.Password == "" {
		log.Println("Encryption set to true, but there's no password")
		fmt.Print("New password: ")
		password, err := term.ReadPassword(syscall.Stdin)
		if err != nil {
			return err
		}
		fmt.Println()
		hasher := sha256.New()
		hasher.Write(password)
		config.Raw.Password = hex.EncodeToString(hasher.Sum(nil))
	}
	return nil
}

func (config *DatboxConfig) Save() error {
	bytes, err := json.MarshalIndent(config.Raw, "", "\t")
	if err != nil {
		return err
	}
	return os.WriteFile(config.configPath, bytes, 0644)
}
