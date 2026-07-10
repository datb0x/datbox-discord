package virtualfile

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"datbox/server/network"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
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
	CopyHeader(header *network.DatboxHeader) error

	Write(data []byte) error
	WriteHeader() error
	WriteMsgID(id uint64) error
	Upload(fileReader io.Reader, size int64, channel chan TransferEvent)

	ReadMsgID() (uint64, error)
	ReadPrevMsgID() (uint64, error)
	Verify(checksum []byte) (bool, error)

	GetNextChunk() ([]byte, error)
	GetNextChunkRaw() ([]byte, error)
	Download(fileWriter io.WriteCloser, channel chan TransferEvent)
}

func CreateVirtualFile(root, path, fsMsg, fsHash string, globalPassword []byte, network *network.DatboxNetwork, fileVersion byte) (VirtualFile, error) {
	if _, err := os.Stat(filepath.Join(root, path)); err == nil {
		return nil, errors.New("File already exists")
	}
	os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0755)
	log.Printf("Creating virtual file with version %d", fileVersion)
	switch fileVersion {
	case 0:
		return NewV0File(root, path, fsMsg, fsHash, network), nil
	case 1:
		return NewV1File(root, path, fsMsg, fsHash, globalPassword, network), nil
	default:
		return nil, errors.New("Unknown file version " + fmt.Sprint(fileVersion))
	}
}

func OpenVirtualFile(root, path, fsMsg, fsHash string, network *network.DatboxNetwork) (VirtualFile, error) {
	stat, err := os.Stat(filepath.Join(root, path))
	if err != nil {
		return nil, err
	}
	if (stat.Size() % 8) == 0 {
		log.Printf("Opening virtual file with version 0")
		return NewV0File(root, path, fsMsg, fsHash, network), nil
	} else {
		file, err := os.Open(filepath.Join(root, path))
		if err != nil {
			return nil, err
		}
		buf := make([]byte, 1)
		_, err = io.ReadFull(file, buf)
		if err != nil {
			return nil, err
		}
		log.Printf("Opening virtual file with version %d", buf[0])
		switch buf[0] {
		case 1:
			return NewV1File(root, path, fsMsg, fsHash, nil, network), nil
		}
	}
	return nil, errors.New("Unknown file version")
}

func ReadFill(r io.Reader, buf []byte) (n int, err error) {
	for n < len(buf) {
		read, er := r.Read(buf[n:])
		n += read

		if er != nil {
			err = er
			return
		}
	}
	return
}

func SymmetricEncrypt(password, plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(password)
	if err != nil {
		return nil, err
	}

	padding := aes.BlockSize - len(plain)%aes.BlockSize
	padBytes := append(plain, bytes.Repeat([]byte{byte(padding)}, padding)...)

	data := make([]byte, aes.BlockSize+len(padBytes))
	iv := data[:aes.BlockSize]
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, err
	}

	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(data[aes.BlockSize:], padBytes)
	return data, nil
}

func SymmetricDecrypt(password, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(password)
	if err != nil {
		return nil, err
	}
	if len(data) < aes.BlockSize {
		return nil, errors.New("Encrypted data is too short")
	}

	plain := make([]byte, len(data))
	copy(plain, data)

	iv := plain[:aes.BlockSize]
	plain = plain[aes.BlockSize:]

	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(plain, plain)

	padding := int(plain[len(plain)-1])
	plain = plain[:len(plain)-padding]
	return plain, nil
}

func endTransfer(channel chan TransferEvent, err error) {
	channel <- TransferEvent{
		Done: true,
		Err:  err,
	}
}

func randomId() string {
	id := make([]byte, 4)
	rand.Read(id)
	return hex.EncodeToString(id)
}
