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
	raw        RawConfig
}

func NewConfig(configPath string, channelId, dataDir string, concurrency int, token string) *DatboxConfig {
	config := new(DatboxConfig)
	config.configPath = configPath
	config.raw.ChannelId = channelId
	config.raw.DataDir = dataDir
	config.raw.Concurrency = concurrency
	config.raw.Token = token
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
			config.raw.ChannelId = rawConfig.ChannelId
		}
		if rawConfig.DataDir != "" {
			config.raw.DataDir = rawConfig.DataDir
		}
		if rawConfig.Concurrency != 0 {
			config.raw.Concurrency = rawConfig.Concurrency
		}
		if rawConfig.Token != "" {
			config.raw.Token = rawConfig.Token
		}
	} else {
		log.Print("Configuration not found. Default settings will be used, except for channelId")
	}
	if config.raw.ChannelId == "" {
		return errors.New("Missing channelId")
	}
	if config.raw.DataDir == "" {
		config.raw.DataDir = path.Join(path.Dir(config.configPath), "root")
	}
	if config.raw.Concurrency <= 0 {
		return errors.New("Invalid concurrency")
	}
	if config.raw.Token == "" {
		return errors.New("Missing token")
	}
	return nil
}

func (config *DatboxConfig) Save() error {
	bytes, err := json.MarshalIndent(config.raw, "", "\t")
	if err != nil {
		return err
	}
	return os.WriteFile(config.configPath, bytes, 0644)
}
