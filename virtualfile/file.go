package virtualfile

import (
	"datbox/network"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
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

func CreateVirtualFile(root, path, fsHash string, network *network.DatboxNetwork, fileVersion byte) (VirtualFile, error) {
	if _, err := os.Stat(filepath.Join(root, path)); err == nil {
		return nil, errors.New("File already exists")
	}
	log.Printf("Creating virtual file with version %d", fileVersion)
	switch fileVersion {
	case 0:
		return NewV0File(root, path, fsHash, network), nil
	case 1:
		return NewV1File(root, path, fsHash, network), nil
	default:
		return nil, errors.New("Unknown file version " + fmt.Sprint(fileVersion))
	}
}

func OpenVirtualFile(root, path, fsHash string, network *network.DatboxNetwork) (VirtualFile, error) {
	stat, err := os.Stat(filepath.Join(root, path))
	if err != nil {
		return nil, err
	}
	if (stat.Size() % 8) == 0 {
		log.Printf("Opening virtual file with version 0")
		return NewV0File(root, path, fsHash, network), nil
	} else {
		file, err := os.Open(filepath.Join(root, path))
		if err != nil {
			return nil, err
		}
		buf := make([]byte, 1)
		read, err := file.Read(buf)
		if err != nil {
			return nil, err
		}
		if read != 1 {
			return nil, errors.New("Did not read 1 byte")
		}
		log.Printf("Opening virtual file with version %d", buf[0])
		switch buf[0] {
		case 1:
			return NewV1File(root, path, fsHash, network), nil
		}
	}
	return nil, errors.New("Unknown file version")
}
