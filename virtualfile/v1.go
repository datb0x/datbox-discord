package virtualfile

import (
	"bytes"
	"compress/flate"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"time"

	datboxcore "github.com/datb0x/datbox-core"
	"github.com/datb0x/datbox-discord/internal"
	"github.com/datb0x/datbox-discord/network"

	"golang.org/x/crypto/blake2b"
)

// V1 Header Structure
// [0]: version int
// [1:4] signature "DtBx"
// [5:36] password
type V1File struct {
	GlobalPassword []byte
	Password       []byte

	V0File
}

func NewV1File(root, path, fsMsg, fsHash string, globalPassword []byte, network *network.DiscordNetwork) *V1File {
	file := new(V1File)
	file.Path = filepath.Join(root, path)
	file.RelPath = path
	file.FileSystemMsg = fsMsg
	file.FileSystemHash = fsHash
	file.network = network
	file.GlobalPassword = globalPassword
	return file
}

func (f *V1File) Version() int {
	return 1
}

func (f *V1File) OpenOrCreate() error {
	f.octoBuf = make([]byte, 8)
	f.Password = make([]byte, 32)
	stat, err := os.Stat(f.Path)
	if err != nil {
		// Not exist
		file, err := os.Create(f.Path)
		if err != nil {
			return err
		}
		f.file = file
		f.writeMode = true
		rand.Read(f.Password)
	} else {
		// Exists
		file, err := os.Open(f.Path)
		if err != nil {
			return err
		}
		f.file = file
		f.writeMode = false
		if (stat.Size() % 8) == 0 {
			return errors.New("File is not version 1")
		}
		// Read header
		buf := make([]byte, 5)
		_, err = io.ReadFull(file, buf)
		if err != nil {
			return err
		}
		// Version number
		if buf[0] != 1 {
			return errors.New("File is not version 1")
		}
		// File signature
		if string(buf[1:5]) != "DtBx" {
			return errors.New("Wrong file signature")
		}
		// File encryption key
		_, err = io.ReadFull(file, f.Password)
		if err != nil {
			return err
		}
		// File size
		_, err = io.ReadFull(file, f.octoBuf)
		if err != nil {
			file.Close()
			return err
		}
		f.size = big.NewInt(0).SetBytes(f.octoBuf).Uint64()
		f.chunks = int(math.Ceil(float64(f.size) / float64(FileChunkSize)))
	}
	return nil
}

func (f *V1File) CopyHeader(header *network.DatboxHeader) (err error) {
	f.Password, err = hex.DecodeString(header.Fields["password"])
	if err != nil {
		return
	}
	if f.GlobalPassword != nil {
		f.Password, err = SymmetricDecrypt(f.GlobalPassword, f.Password)
		if err != nil {
			return
		}
	}
	return
}

func (f *V1File) WriteHeader() error {
	// Write file signature
	f.file.Write([]byte{1})
	f.file.Write([]byte("DtBx"))
	// Write encryption key
	f.file.Write(f.Password)
	// Write size
	big.NewInt(int64(f.size)).FillBytes(f.octoBuf)
	_, err := f.file.Write(f.octoBuf)
	if err != nil {
		return err
	}
	return nil
}

func (f *V1File) Upload(fileReader io.Reader, size int64, progressCallback func(datboxcore.Progress)) error {
	if f.file == nil {
		return errors.New("No file opened")
	}

	f.size = uint64(size)
	startTime := time.Now()

	// Begin header
	header := network.NewHeader(network.ActionBegin)
	header.Fields["fs-msg"] = f.FileSystemMsg
	header.Fields["fs-hash"] = f.FileSystemHash
	header.Fields["path"] = f.RelPath
	header.Fields["version"] = "1"
	header.Fields["size"] = fmt.Sprint(f.size)

	// Encrypt password if necessary
	var filePassword []byte
	var err error
	if f.GlobalPassword != nil {
		filePassword, err = SymmetricEncrypt(f.GlobalPassword, f.Password)
		if err != nil {
			return err
		}
	} else {
		filePassword = f.Password
	}
	header.Fields["password"] = hex.EncodeToString(filePassword)

	err = f.network.SendMessage(header)
	if err != nil {
		return err
	}

	// Delete fields for next header
	delete(header.Fields, "size")
	delete(header.Fields, "password")

	// Chunk header
	header.Action = network.ActionFileChunk

	// Write file header
	f.WriteHeader()

	// Setup variables
	buf := make([]byte, FileChunkSize)
	estimatedChunks := int(math.Ceil(float64(size) / FileChunkSize))
	header.Fields["chunks"] = fmt.Sprint(estimatedChunks)

	// Piggyback file checksum
	hasher, err := blake2b.New256([]byte("DtBx"))
	if err != nil {
		return err
	}

	futures := internal.NewQueue[internal.Future[int64]](f.network.Uploader.Concurrency)
	readBytes := make(chan int, f.network.Uploader.Concurrency)
	index := 0

	dequeueFuture := func() error {
		future := futures.Dequeue()
		id, err := future.Await()
		if err != nil {
			return err
		}
		f.chunks++
		big.NewInt(*id).FillBytes(f.octoBuf)
		f.file.Write(f.octoBuf)
		return nil
	}

	// Progress updater
	go func() {
		total := size
		totalBytes := int64(0)
		for totalBytes < total {
			read := <-readBytes
			totalBytes += int64(read)
			progressCallback(datboxcore.Progress{
				StartTime:     startTime,
				CurrentBytes:  totalBytes,
				TotalBytes:    total,
				CurrentChunks: f.chunks,
				TotalChunks:   estimatedChunks,
			})
		}
	}()

	for {
		read, err := ReadFill(fileReader, buf)
		if err != nil && err != io.EOF {
			return err
		}
		// Update hash
		hasher.Write(buf[:read])

		// Deflate data
		pipeReader, pipeWriter := io.Pipe()
		go func() {
			flateWriter, err := flate.NewWriter(pipeWriter, flate.DefaultCompression)
			if err != nil {
				flateWriter.Close()
			}
			flateWriter.Write(buf[:read])
			flateWriter.Close()
			pipeWriter.Close()
		}()
		compressed, err := io.ReadAll(pipeReader)
		if err != nil {
			return err
		}

		// Encrypt data
		data, err := SymmetricEncrypt(f.Password, compressed)
		if err != nil {
			return err
		}

		// Send to Discord
		for futures.IsFull() {
			err = dequeueFuture()
			if err != nil {
				return err
			}
		}
		header.Fields["index"] = fmt.Sprint(index)
		headerStr := header.String()
		index++
		futures.Enqueue(internal.NewFuture(func() (int64, error) {
			id, err := f.network.Uploader.SendAttachment(data, headerStr, false)
			if err != nil {
				return 0, err
			}
			parsed, err := strconv.ParseUint(id, 10, 64)
			readBytes <- read
			return int64(parsed), err
		}), false)

		if read != len(buf) {
			break
		}
	}

	for !futures.IsEmpty() {
		dequeueFuture()
	}

	// Write separator
	big.NewInt(0).FillBytes(f.octoBuf)
	f.file.Write(f.octoBuf)
	// Write file checksum at the end
	f.checksum = hasher.Sum(nil)
	f.file.Write(f.checksum)

	// End header
	header = network.NewHeader(network.ActionComplete)
	header.Fields["fs-msg"] = f.FileSystemMsg
	header.Fields["fs-hash"] = f.FileSystemHash
	header.Fields["path"] = f.RelPath
	header.Fields["checksum"] = hex.EncodeToString(f.checksum)
	return f.network.SendMessage(header)
}

func (f *V1File) Verify(checksum []byte) (bool, error) {
	if f.file == nil {
		return false, errors.New("No file opened")
	}
	buf := make([]byte, len(checksum))
	f.file.Seek(-32, io.SeekEnd)
	read, err := f.file.Read(buf)
	if read != 32 || err != nil && err != io.EOF {
		return false, errors.New("Virtual file is corrupted")
	}
	for ii := range 32 {
		if buf[ii] != checksum[ii] {
			return false, nil
		}
	}
	return true, nil
}

func (f *V1File) GetNextChunk() ([]byte, error) {
	data, err := f.GetNextChunkRaw()
	if err != nil {
		return nil, err
	}

	// Decrypt
	data, err = SymmetricDecrypt(f.Password, data)
	// Inflate
	return io.ReadAll(flate.NewReader(bytes.NewReader(data)))
}

func (f *V1File) Download(fileWriter io.WriteCloser, progressCallback func(datboxcore.Progress)) error {
	if f.file == nil {
		return errors.New("No file opened")
	}
	defer fileWriter.Close()

	hasher, err := blake2b.New256([]byte("DtBx"))
	if err != nil {
		return err
	}
	estimatedChunks := int(math.Ceil(float64(f.size) / float64(FileChunkSize)))
	chunks := 0
	startTime := time.Now()

	var data []byte
	totalBytes := 0
	for {
		data, err = f.GetNextChunk()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		hasher.Write(data)
		fileWriter.Write(data)
		totalBytes += len(data)
		chunks++
		progressCallback(datboxcore.Progress{
			StartTime:     startTime,
			CurrentBytes:  int64(totalBytes),
			TotalBytes:    f.Size(),
			CurrentChunks: chunks,
			TotalChunks:   estimatedChunks,
		})
	}

	matched, err := f.Verify(hasher.Sum(nil))
	if err != nil {
		return err
	}
	if !matched {
		return errors.New("Downloaded file checksum doesn't match")
	}
	return nil
}
