package server

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/bwmarrin/discordgo"
)

type DatboxNetwork struct {
	channelId string
	session   *discordgo.Session
	channel   *discordgo.Channel
}

func NewNetwork(token string, channelId string) (*DatboxNetwork, error) {
	network := new(DatboxNetwork)
	network.channelId = channelId
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return network, err
	}
	session.Open()
	log.Printf("%s is ready\n", session.State.User.Username)
	network.session = session
	network.channel, err = session.Channel(channelId)
	if err != nil {
		return network, err
	}
	if network.channel.Type != discordgo.ChannelTypeGuildText {
		return network, errors.New("Channel is not guild-text")
	}
	return network, nil
}

func (network *DatboxNetwork) SendAttachment(data []byte) (string, error) {
	hasher := md5.New()
	hasher.Write(data)
	hash := hex.EncodeToString(hasher.Sum(nil))
	message, err := network.session.ChannelFileSend(network.channelId, hash, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	return message.ID, nil
}

func (network *DatboxNetwork) FetchAttachment(id string) ([]byte, error) {
	message, err := network.session.ChannelMessage(network.channelId, id)
	if err != nil {
		return nil, err
	}
	if len(message.Attachments) < 1 {
		return nil, errors.New("Message has no attachment")
	}
	resp, err := http.Get(message.Attachments[0].URL)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(resp.Body)
}

func (network *DatboxNetwork) DeleteMessages(ids []string) {
	bulk := []string{}
	individual := []string{}
	for _, id := range ids {
		snowflakeTime, err := discordgo.SnowflakeTimestamp(id)
		if err != nil || time.Now().After(snowflakeTime.Add(time.Hour*24*14)) {
			individual = append(individual, id)
		} else {
			bulk = append(bulk, id)
		}
	}
	if len(bulk) > 0 {
		network.session.ChannelMessagesBulkDelete(network.channelId, bulk)
	}
	for _, id := range individual {
		network.session.ChannelMessageDelete(network.channelId, id)
	}
}
