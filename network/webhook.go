package network

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"sync"

	"github.com/bwmarrin/discordgo"
	"github.com/typical-developers/discord-webhooks-go/webhooks"
	"golang.org/x/sync/semaphore"
)

type ClientWrapper struct {
	client *webhooks.WebhookClient
	mu     sync.Mutex
}

type ConcurrentUploader struct {
	Concurrency int
	network     *DatboxNetwork
	webhooks    []*ClientWrapper
	semaphore   *semaphore.Weighted
}

func NewConcurrentUploader(network *DatboxNetwork, concurrency int) (*ConcurrentUploader, error) {
	log.Printf("Creating concurrent uploader with concurrency %d...", concurrency)
	if concurrency <= 1 {
		return &ConcurrentUploader{
			Concurrency: concurrency,
			network:     network,
		}, nil
	}

	channelWebhooks, err := network.session.ChannelWebhooks(network.channelId)
	if err != nil {
		return nil, err
	}

	if len(channelWebhooks) < concurrency {
		log.Printf("Channel doesn't have enough webhooks. Creating %d new webhooks...", concurrency-len(channelWebhooks))
		for len(channelWebhooks) < concurrency {
			webhook, err := network.session.WebhookCreate(network.channelId, fmt.Sprintf("concurrent-uploader #%d", len(channelWebhooks)), "")
			if err != nil {
				return nil, err
			}
			channelWebhooks = append(channelWebhooks, webhook)
		}
	} else {
		log.Print("Channel has enough webhooks")
	}

	uploader := new(ConcurrentUploader)
	uploader.network = network
	uploader.Concurrency = concurrency
	uploader.semaphore = semaphore.NewWeighted(int64(concurrency))
	for _, webhook := range channelWebhooks {
		client := webhooks.NewWebhookClient(webhook.ID, webhook.Token)
		uploader.webhooks = append(uploader.webhooks, &ClientWrapper{
			client: client,
			mu:     sync.Mutex{},
		})
	}

	return uploader, nil
}

func (uploader *ConcurrentUploader) SendAttachment(data []byte, content string, useSession bool) (string, error) {
	hasher := md5.New()
	hasher.Write(data)
	hash := hex.EncodeToString(hasher.Sum(nil))

	// DatboxNetwork only exists if we don't use webhooks
	if uploader.Concurrency == 1 || useSession {
		messageSend := discordgo.MessageSend{
			Content: content,
			Files: []*discordgo.File{{
				Name:   hash,
				Reader: bytes.NewReader(data),
			}},
		}
		message, err := uploader.network.session.ChannelMessageSendComplex(uploader.network.channelId, &messageSend)
		if err != nil {
			return "", err
		}
		return message.ID, nil
	}

	// Acquire semaphore
	err := uploader.semaphore.Acquire(context.Background(), 1)
	if err != nil {
		return "", err
	}
	defer uploader.semaphore.Release(1)

	var wrapper *ClientWrapper
	for _, webhook := range uploader.webhooks {
		if webhook.mu.TryLock() {
			wrapper = webhook
			break
		}
	}
	defer wrapper.mu.Unlock()
	response, err := wrapper.client.SendMessage(&webhooks.WebhookPayload{
		Content: &content,
		Files: []*webhooks.WebhookFile{
			&webhooks.WebhookFile{
				Name:   hash,
				Reader: io.NopCloser(bytes.NewReader(data)),
			},
		},
	})
	if err != nil {
		return "", err
	}
	return response.MessageID, nil
}
