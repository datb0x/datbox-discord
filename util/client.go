package util

import (
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"strings"

	ipc "github.com/james-barrow/golang-ipc"
)

var (
	ErrNoSendData = errors.New("No sendData")
)

type IPCClient struct {
	id       int32
	client   *ipc.Client
	sendData *DataWrapper
}

func NewClientWrapper() (*IPCClient, error) {
	client, err := ipc.StartClient("datbox", &ipc.ClientConfig{
		RetryTimer: 1,
	})
	if err != nil {
		return nil, err
	}
	id := rand.Int32()
	for id >= -1 && id <= 1 {
		id = rand.Int32()
	}
	ipcClient := IPCClient{
		id:     id,
		client: client,
	}
	return &ipcClient, nil
}

func NewClientWrapperAndConnect() (client *IPCClient, err error) {
	client, err = NewClientWrapper()
	if err != nil {
		return
	}
	err = client.Connect()
	return
}

func (ipcClient *IPCClient) Connect() error {
	for message, err := ipcClient.client.Read(); err != nil || message.MsgType == -1; {
		if err != nil {
			return err
		}
		if message.Err != nil {
			return message.Err
		}
		if ipcClient.client.StatusCode() == ipc.Connected {
			// need to read once more
			ipcClient.client.Read()
			break
		}
	}
	return nil
}

func (ipcClient *IPCClient) Close() {
	ipcClient.client.Close()
}

func (ipcClient *IPCClient) Next() (data []byte, header byte, err error) {
	var message *ipc.Message
	for {
		message, err = ipcClient.client.Read()
		if err != nil {
			return
		}
		if message.MsgType == -1 {
			err = io.EOF
			return
		} else if message.MsgType != int(ipcClient.id) {
			continue
		}
		data = message.Data[1:]
		header = message.Data[0]
		return
	}
}

func (ipcClient *IPCClient) Prepare() *DataWrapper {
	if ipcClient.sendData == nil {
		ipcClient.sendData = NewDataWrapper(make([]byte, 0))
	} else {
		ipcClient.sendData.Clear()
	}
	ipcClient.sendData.WriteInt32(ipcClient.id)
	return ipcClient.sendData
}

func (ipcClient *IPCClient) Send(msgType int) error {
	if ipcClient.sendData == nil {
		return ErrNoSendData
	}
	return ipcClient.client.Write(msgType, ipcClient.sendData.data)
}

func (ipcClient *IPCClient) ReadAllMsgs(writers ...io.Writer) error {
	for {
		data, header, err := ipcClient.Next()
		if err != nil {
			return err
		}
		if header == ServerHeaderRaw {
			for _, writer := range writers {
				_, err := writer.Write(data)
				if err != nil {
					return err
				}
			}
		} else {
			str := string(data)
			if str != "" {
				if strings.ContainsAny(str, "\r\n") {
					fmt.Print(str)
				} else {
					fmt.Println(str)
				}
			}
			if header == ServerHeaderSuccess || header == ServerHeaderFailure {
				break
			}
		}
	}
	return nil
}
