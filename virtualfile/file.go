package virtualfile

import (
	"datbox/network"
	"errors"
	"os"
)

const FileChunkSize = 10 * 1024 * 1023

type TransferEvent struct {
	Done    bool
	Err     error
	Current int64
	Total   int64
}

type VirtualFile interface {
	Version() int
	Chunks() int
	Size() int64
	WriteMode() bool
	Checksum() []byte

	OpenOrCreate() error
	Close() error

	WriteMsgID(id uint64) error
	UploadFrom(path string, channel chan TransferEvent)

	ReadMsgID() (uint64, error)
	ReadPrevMsgID() (uint64, error)
	Verify(checksum []byte) (bool, error)

	GetNextChunk() ([]byte, error)
	GetNextChunkRaw() ([]byte, error)
	DownloadTo(path string, channel chan TransferEvent)
}

func CreateVirtualFile(path string, network *network.DatboxNetwork) (VirtualFile, error) {
	if _, err := os.Stat(path); err == nil {
		return nil, errors.New("File already exists")
	}
	return NewV0File(path, network), nil
}

func OpenVirtualFile(path string, network *network.DatboxNetwork) (VirtualFile, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if (stat.Size() % 8) == 0 {
		return NewV0File(path, network), nil
	}
	return nil, errors.New("Unknown file version")
}
