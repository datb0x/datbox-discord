package comm

import (
	ipc "github.com/james-barrow/golang-ipc"
)

const (
	LogHeaderSuccess      = 0
	LogHeaderFailure      = 1
	LogHeaderIntermediate = 3
	LogHeaderRaw          = 4
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
		for {
			message := <-logger.messages
			server.Write(id, message)
			if message[0] == LogHeaderSuccess || message[0] == LogHeaderFailure {
				break
			}
		}
	}()

	return &logger
}

func (logger *IPCLogger) sendData(header byte, data []byte) {
	logger.messages <- append([]byte{header}, data...)
}

func (logger *IPCLogger) SendSuccess(message string) {
	go logger.sendData(LogHeaderSuccess, []byte(message))
}

func (logger *IPCLogger) SendFailure(message string) {
	go logger.sendData(LogHeaderFailure, []byte(message))
}

func (logger *IPCLogger) SendIntermediate(message string) {
	go logger.sendData(LogHeaderIntermediate, []byte(message))
}

func (logger *IPCLogger) SendRaw(data []byte) {
	logger.sendData(LogHeaderRaw, data)
}
