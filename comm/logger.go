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

func (logger *IPCLogger) SendSuccess(message string, blocking ...bool) {
	if len(blocking) > 0 && blocking[0] {
		logger.sendData(LogHeaderSuccess, []byte(message))
	} else {
		go logger.sendData(LogHeaderSuccess, []byte(message))
	}
}

func (logger *IPCLogger) SendFailure(message string, blocking ...bool) {
	if len(blocking) > 0 && blocking[0] {
		logger.sendData(LogHeaderFailure, []byte(message))
	} else {
		go logger.sendData(LogHeaderFailure, []byte(message))
	}
}

func (logger *IPCLogger) SendIntermediate(message string, blocking ...bool) {
	if len(blocking) > 0 && blocking[0] {
		logger.sendData(LogHeaderIntermediate, []byte(message))
	} else {
		go logger.sendData(LogHeaderIntermediate, []byte(message))
	}
}

func (logger *IPCLogger) SendRaw(data []byte, blocking ...bool) {
	if len(blocking) > 0 && blocking[0] {
		logger.sendData(LogHeaderRaw, data)
	} else {
		go logger.sendData(LogHeaderRaw, data)
	}
}
