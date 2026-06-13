package comm

import (
	"sync"

	ipc "github.com/james-barrow/golang-ipc"
)

const (
	LogHeaderSuccess      = 0
	LogHeaderFailure      = 1
	LogHeaderDiscardable  = 2
	LogHeaderIntermediate = 3
)

type IPCLogger struct {
	server   *ipc.Server
	id       int
	messages chan []byte
}

func NewLogger(server *ipc.Server, id int) *IPCLogger {
	logger := IPCLogger{
		server:   server,
		id:       id,
		messages: make(chan []byte),
	}

	go func() {
		mutex := sync.Mutex{}
		for {
			message := <-logger.messages
			switch message[0] {
			case LogHeaderSuccess, LogHeaderFailure:
				mutex.Lock()
				server.Write(id, message)
				mutex.Unlock()
				return
			case LogHeaderDiscardable:
				go func() {
					if mutex.TryLock() {
						logger.server.Write(id, message)
						mutex.Unlock()
					}
				}()
			case LogHeaderIntermediate:
				mutex.Lock()
				server.Write(id, message)
				mutex.Unlock()
			}
		}
	}()

	return &logger
}

func (logger *IPCLogger) sendData(header byte, data []byte) {
	logger.messages <- append([]byte{header}, data...)
}

func (logger *IPCLogger) SendSuccess(data []byte) {
	go logger.sendData(LogHeaderSuccess, data)
}

func (logger *IPCLogger) SendFailure(data []byte) {
	go logger.sendData(LogHeaderFailure, data)
}

func (logger *IPCLogger) SendDiscardable(data []byte) {
	go logger.sendData(LogHeaderDiscardable, data)
}

func (logger *IPCLogger) SendIntermediate(data []byte) {
	go logger.sendData(LogHeaderIntermediate, data)
}
