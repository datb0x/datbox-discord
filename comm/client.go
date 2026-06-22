package comm

import (
	"fmt"
	"math/rand/v2"
	"strings"

	ipc "github.com/james-barrow/golang-ipc"
)

func StartClientAndWait() (*ipc.Client, int32, error) {
	client, err := ipc.StartClient("datbox", &ipc.ClientConfig{
		RetryTimer: 1,
	})
	if err != nil {
		return nil, 0, err
	}
	for message, err := client.Read(); err != nil || message.MsgType == -1; {
		if err != nil {
			return nil, 0, err
		}
		if message.Err != nil {
			return nil, 0, message.Err
		}
		if client.StatusCode() == ipc.Connected {
			// need to read once more
			client.Read()
			break
		}
	}
	id := rand.Int32()
	for id >= -1 && id <= 1 {
		id = rand.Int32()
	}
	return client, id, nil
}

func WaitForMessage(client *ipc.Client, id int32) (*IPCReader, byte, error) {
	for {
		message, err := client.Read()
		if err != nil {
			return nil, 1, err
		}
		if message.MsgType != int(id) {
			continue
		}
		reader := NewReader(message)
		status, err := reader.ReadByte()
		if err != nil {
			return nil, 1, err
		}
		return reader, status, nil
	}
}

func ReadUntilEnd(client *ipc.Client, id int32) error {
	for {
		message, err := client.Read()
		if err != nil {
			return err
		}
		if message.MsgType != int(id) {
			continue
		}
		str := string(message.Data[1:])
		if str != "" {
			if strings.ContainsAny(str, "\r") {
				fmt.Print(str)
			} else {
				fmt.Println(str)
			}
		}
		status := message.Data[0]
		if status == 0 || status == 1 {
			break
		}
	}
	return nil
}
