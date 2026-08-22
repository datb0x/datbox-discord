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
	"os"
)

const FileChunkSize = 10 * 1024 * 1023

type TransferEvent struct {
	Done    bool
	Err     error
	Current int64
	Total   int64
}

type fsData struct {
	Root     string
	RelPath  string
	MsgID    string
	Hash     string
	Password []byte
}

type VirtualFile interface {
	Version() int
	Chunks() int
	Size() int64

	CopyHeader(header *network.DatboxHeader) error

	WriteHeader() error

	ReadMsgID() (uint64, error)

	ReadChunk(index int64) ([]byte, error)
	ReadChunkRaw(index int64) ([]byte, error)
	WriteChunk(index int64, data []byte) (uint64, error)
	Raw() *os.File

	io.Closer
	io.Reader
	io.Seeker
	io.Writer
}

func NewVirtualFile(file *os.File, root, path, fsMsg, fsHash string, globalPassword []byte, network *network.DatboxNetwork, fileVersion int) (VirtualFile, error) {
	fsData := fsData{
		Root:     root,
		RelPath:  path,
		MsgID:    fsMsg,
		Hash:     fsHash,
		Password: globalPassword,
	}
	if fileVersion == -1 {
		buf := make([]byte, 1)
		read, err := file.Read(buf)
		if err != nil || read != 1 {
			return nil, errors.New("File version unspecified, but file does not exist")
		}
		fileVersion = int(buf[0])
	}
	switch fileVersion {
	case 0:
		return NewV0File(file, fsData, network)
	case 1:
		return NewV1File(file, fsData, network)
	default:
		return nil, errors.New("Unknown file version " + fmt.Sprint(fileVersion))
	}
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
