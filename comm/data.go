package comm

import (
	"encoding/binary"
	"io"
	"math"
)

type DataWrapper struct {
	data []byte
	ptr  int
}

func NewDataWrapper(data []byte) *DataWrapper {
	return &DataWrapper{
		data: data,
		ptr:  0,
	}
}

// Read functions
func (wrapper *DataWrapper) ReadNBytes(n int) ([]byte, error) {
	if wrapper.ptr >= len(wrapper.data) && n > 0 {
		wrapper.ptr = len(wrapper.data)
		return nil, io.EOF
	} else if wrapper.ptr+n >= len(wrapper.data) {
		data := wrapper.data[wrapper.ptr:len(wrapper.data)]
		wrapper.ptr = len(wrapper.data)
		return data, nil
	} else {
		data := wrapper.data[wrapper.ptr : wrapper.ptr+n]
		wrapper.ptr += n
		return data, nil
	}
}

func (wrapper *DataWrapper) ReadByte() (byte, error) {
	data, err := wrapper.ReadNBytes(1)
	if err != nil {
		return 0, err
	}
	return data[0], nil
}

func (wrapper *DataWrapper) ReadBool() (bool, error) {
	data, err := wrapper.ReadByte()
	if err != nil {
		return false, err
	}
	return data == 1, err
}

func (wrapper *DataWrapper) ReadUInt16() (uint16, error) {
	data, err := wrapper.ReadNBytes(2)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(data), nil
}

func (wrapper *DataWrapper) ReadInt32() (int32, error) {
	data, err := wrapper.ReadUInt32()
	if err != nil {
		return 0, err
	}
	return int32(data), err
}

func (wrapper *DataWrapper) ReadUInt32() (uint32, error) {
	data, err := wrapper.ReadNBytes(4)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(data), err
}

func (wrapper *DataWrapper) ReadUInt64() (uint64, error) {
	data, err := wrapper.ReadNBytes(8)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(data), nil
}

func (wrapper *DataWrapper) ReadFloat32() (float32, error) {
	data, err := wrapper.ReadUInt32()
	if err != nil {
		return 0, err
	}
	return math.Float32frombits(data), nil
}

func (wrapper *DataWrapper) ReadUtf8() (string, error) {
	length, err := wrapper.ReadUInt16()
	if err != nil {
		return "", err
	}
	buffer, err := wrapper.ReadNBytes(int(length))
	if err != nil {
		return "", err
	}
	return string(buffer), nil
}

// Write functions
func (wrapper *DataWrapper) WriteBytes(val []byte) {
	if wrapper.ptr >= len(wrapper.data) {
		wrapper.data = append(wrapper.data, val...)
		wrapper.ptr = len(wrapper.data)
	} else if wrapper.ptr+len(val) >= len(wrapper.data) {
		copy(wrapper.data[wrapper.ptr:], val[:len(wrapper.data)-wrapper.ptr])
		wrapper.data = append(wrapper.data, val[len(wrapper.data)-wrapper.ptr:]...)
		wrapper.ptr = len(wrapper.data)
	} else {
		copy(wrapper.data[wrapper.ptr:], val)
		wrapper.ptr += len(val)
	}
}

func (wrapper *DataWrapper) WriteByte(val byte) {
	wrapper.WriteBytes([]byte{val})
}

func (wrapper *DataWrapper) WriterBool(val bool) {
	if val {
		wrapper.WriteBytes([]byte{1})
	} else {
		wrapper.WriteBytes([]byte{0})
	}
}

func (wrapper *DataWrapper) WriteUInt16(val uint16) {
	wrapper.WriteBytes(binary.BigEndian.AppendUint16(nil, val))
}

func (wrapper *DataWrapper) WriteInt32(val int32) {
	buffer := []byte{
		byte(val >> 24), // MSB
		byte(val >> 16),
		byte(val >> 8),
		byte(val), // LSB
	}
	wrapper.WriteBytes(buffer)
}

func (wrapper *DataWrapper) WriteUInt32(val uint32) {
	wrapper.WriteBytes(binary.BigEndian.AppendUint32(nil, val))
}

func (wrapper *DataWrapper) WriteUInt64(val uint64) {
	wrapper.WriteBytes(binary.BigEndian.AppendUint64(nil, val))
}

func (wrapper *DataWrapper) WriteFloat32(val float32) {
	wrapper.WriteUInt32(math.Float32bits(val))
}

func (wrapper *DataWrapper) WriteUtf8(val string) {
	data := []byte(val)
	length := len(data)
	wrapper.WriteUInt16(uint16(length))
	wrapper.WriteBytes(data)
}

// Utility
func (wrapper *DataWrapper) Clear() {
	wrapper.data = make([]byte, 0)
	wrapper.ptr = 0
}

func (wrapper *DataWrapper) Seek(offset, whence int) {
	switch whence {
	case io.SeekStart:
		wrapper.ptr = offset
	case io.SeekCurrent:
		wrapper.ptr += offset
	case io.SeekEnd:
		wrapper.ptr = len(wrapper.data) - offset
	}
}
