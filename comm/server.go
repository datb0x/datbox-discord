package comm

import (
	ipc "github.com/james-barrow/golang-ipc"
)

const (
	ServerHeaderSuccess      = 0
	ServerHeaderFailure      = 1
	ServerHeaderIntermediate = 3
	ServerHeaderRaw          = 4
)

type IPCServer struct {
	ID       int
	server   *ipc.Server
	messages chan []byte
}

func NewServerWrapper(server *ipc.Server, id int) *IPCServer {
	ipcServer := IPCServer{
		ID:       id,
		server:   server,
		messages: make(chan []byte),
	}

	ipcServer.Reset()

	return &ipcServer
}

func (ipcServer *IPCServer) Reset() {
	go func() {
		for {
			message := <-ipcServer.messages
			ipcServer.server.Write(ipcServer.ID, message)
			if message[0] == ServerHeaderSuccess || message[0] == ServerHeaderFailure {
				break
			}
		}
	}()
}

func (ipcServer *IPCServer) sendData(header byte, data []byte) {
	ipcServer.messages <- append([]byte{header}, data...)
}

func (ipcServer *IPCServer) SendSuccess(message string, blocking ...bool) {
	if len(blocking) > 0 && blocking[0] {
		ipcServer.sendData(ServerHeaderSuccess, []byte(message))
	} else {
		go ipcServer.sendData(ServerHeaderSuccess, []byte(message))
	}
}

func (ipcServer *IPCServer) SendFailure(message string, blocking ...bool) {
	if len(blocking) > 0 && blocking[0] {
		ipcServer.sendData(ServerHeaderFailure, []byte(message))
	} else {
		go ipcServer.sendData(ServerHeaderFailure, []byte(message))
	}
}

func (ipcServer *IPCServer) SendIntermediate(message string, blocking ...bool) {
	if len(blocking) > 0 && blocking[0] {
		ipcServer.sendData(ServerHeaderIntermediate, []byte(message))
	} else {
		go ipcServer.sendData(ServerHeaderIntermediate, []byte(message))
	}
}

func (ipcServer *IPCServer) SendRaw(data []byte, blocking ...bool) {
	if len(blocking) > 0 && blocking[0] {
		ipcServer.sendData(ServerHeaderRaw, data)
	} else {
		go ipcServer.sendData(ServerHeaderRaw, data)
	}
}
