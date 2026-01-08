package comm

import (
	"encoding/binary"
)

type IPCWriter struct {
	Data []byte
}

func NewWriter() *IPCWriter {
	return &IPCWriter{
		Data: make([]byte, 0),
	}
}

func (ipc *IPCWriter) WriteByte(val byte) {
	ipc.Data = append(ipc.Data, val)
}

func (ipc *IPCWriter) WriteBytes(val []byte) {
	ipc.Data = append(ipc.Data, val...)
}

func (ipc *IPCWriter) WriteUInt16(val uint16) {
	ipc.Data = binary.BigEndian.AppendUint16(ipc.Data, val)
}

func (ipc *IPCWriter) WriteInt32(val int32) {
	buffer := []byte{
		byte(val >> 24), // MSB
		byte(val >> 16),
		byte(val >> 8),
		byte(val), // LSB
	}
	ipc.Data = append(ipc.Data, buffer...)
}

func (ipc *IPCWriter) WriteUInt32(val uint32) {
	ipc.Data = binary.BigEndian.AppendUint32(ipc.Data, val)
}

func (ipc *IPCWriter) WriteUInt64(val uint64) {
	ipc.Data = binary.BigEndian.AppendUint64(ipc.Data, val)
}

func (ipc *IPCWriter) WriteUtf8(val string) {
	length := len(val)
	ipc.WriteUInt16(uint16(length))
	ipc.WriteBytes([]byte(val))
}

func (ipc *IPCWriter) Clear() {
	ipc.Data = make([]byte, 0)
}
