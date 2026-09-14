package network

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/datb0x/datbox-discord/internal"
)

type DiscordNetwork struct {
	channelId string
	session   *discordgo.Session
	channel   *discordgo.Channel
	Uploader  *ConcurrentUploader
}

func NewNetwork(token string, channelId string, concurrency int) (*DiscordNetwork, error) {
	network := new(DiscordNetwork)
	network.channelId = channelId
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, err
	}
	err = session.Open()
	if err != nil {
		return nil, err
	}
	internal.Logger.Debugf("%s is ready\n", session.State.User.Username)
	network.session = session
	network.channel, err = session.Channel(channelId)
	if err != nil {
		return network, err
	}
	if network.channel.Type != discordgo.ChannelTypeGuildText {
		return network, errors.New("Channel is not guild-text")
	}
	network.Uploader, err = NewConcurrentUploader(network, concurrency)
	if err != nil {
		return network, err
	}
	return network, nil
}

func (network *DiscordNetwork) SendMessage(header *DatboxHeader) error {
	_, err := network.session.ChannelMessageSend(network.channelId, header.String())
	if err != nil {
		return err
	}
	return nil
}

func (network *DiscordNetwork) SendAttachment(data []byte, content string) (string, error) {
	return network.Uploader.SendAttachment(data, content, true)
}

func (network *DiscordNetwork) FetchAttachment(id string) ([]byte, error) {
	message, err := network.session.ChannelMessage(network.channelId, id)
	if err != nil {
		return nil, err
	}
	if len(message.Attachments) < 1 {
		return nil, fmt.Errorf("Message has no attachment: %s", id)
	}
	resp, err := http.Get(message.Attachments[0].URL)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(resp.Body)
}

func (network *DiscordNetwork) DeleteMessages(ids []string) {
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
		internal.Logger.Debugf("Bulk deleting the following messages: %s\n", strings.Join(bulk, ", "))
		err := network.session.ChannelMessagesBulkDelete(network.channelId, bulk)
		if err != nil {
			internal.Logger.Errorf("Remote bulk delete failed", err)
		}
	}
	internal.Logger.Debugf("Deleting the following messages individually: %s\n", strings.Join(individual, ", "))
	for _, id := range individual {
		err := network.session.ChannelMessageDelete(network.channelId, id)
		if err != nil {
			internal.Logger.Errorf("Remote delete failed", err)
		}
	}
}

func (network *DiscordNetwork) FetchLastMessage() (*discordgo.Message, error) {
	internal.Logger.Debugf("Fetching last message from channel %s", network.channelId)
	messages, err := network.session.ChannelMessages(network.channelId, 1, "", "", "")
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, nil
	}
	return messages[0], nil
}

func (network *DiscordNetwork) FetchMessagesSince(afterID string, limit int) ([]*discordgo.Message, error) {
	messages, err := network.session.ChannelMessages(network.channelId, limit, "", afterID, "")
	if err != nil {
		return nil, err
	}
	return messages, nil
}

func (network *DiscordNetwork) FetchMessage(ID string) (*discordgo.Message, error) {
	message, err := network.session.ChannelMessage(network.channelId, ID)
	if err != nil {
		return nil, err
	}
	return message, nil
}
