package server

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"path"
)

type RawConfig struct {
	ChannelId   string `json:"channelId"`
	DataDir     string `json:"dataDir"`
	Concurrency int    `json:"concurrency"`
	Token       string `json:"token"`
}

type DatboxConfig struct {
	configPath string
	Raw        RawConfig
}

func NewConfig(configPath string, channelId, dataDir string, concurrency int, token string) *DatboxConfig {
	config := new(DatboxConfig)
	config.configPath = configPath
	config.Raw.ChannelId = channelId
	config.Raw.DataDir = dataDir
	config.Raw.Concurrency = concurrency
	config.Raw.Token = token
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
	return nil
}

func (config *DatboxConfig) Save() error {
	bytes, err := json.MarshalIndent(config.Raw, "", "\t")
	if err != nil {
		return err
	}
	return os.WriteFile(config.configPath, bytes, 0644)
}
