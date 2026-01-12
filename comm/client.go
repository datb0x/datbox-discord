package comm

import (
	"math/rand/v2"

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
