package comm

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"

	ipc "github.com/james-barrow/golang-ipc"
)

type IPCReader struct {
	reader io.Reader
}

func NewReader(message *ipc.Message) *IPCReader {
	return &IPCReader{
		reader: bytes.NewReader(message.Data),
	}
}

func (ipc *IPCReader) ReadByte() (byte, error) {
	buffer, err := ipc.ReadNBytes(1)
	if err != nil {
		return 0, err
	}
	return buffer[0], err
}

func (ipc *IPCReader) ReadNBytes(n int) ([]byte, error) {
	buffer := make([]byte, n)
	read, err := ipc.reader.Read(buffer)
	if err != nil {
		return nil, err
	}
	if read != n {
		return nil, errors.New("Did not read 4 bytes")
	}
	return buffer, nil
}

func (ipc *IPCReader) ReadUInt16() (uint16, error) {
	buffer, err := ipc.ReadNBytes(2)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(buffer), nil
}

func (ipc *IPCReader) ReadInt32() (int32, error) {
	val, err := ipc.ReadUInt32()
	if err != nil {
		return 0, err
	}
	return int32(val), err
}

func (ipc *IPCReader) ReadUInt32() (uint32, error) {
	buffer, err := ipc.ReadNBytes(4)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(buffer), nil
}

func (ipc *IPCReader) ReadUInt64() (uint64, error) {
	buffer, err := ipc.ReadNBytes(8)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(buffer), nil
}

func (ipc *IPCReader) ReadUtf8() (string, error) {
	length, err := ipc.ReadUInt16()
	if err != nil {
		return "", err
	}
	buffer, err := ipc.ReadNBytes(int(length))
	if err != nil {
		return "", err
	}
	return string(buffer), nil
}
