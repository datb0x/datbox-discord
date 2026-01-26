package virtualfile

import (
	"bytes"
	"compress/flate"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"datbox/network"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"math/big"
	"os"
	"strconv"

	"golang.org/x/crypto/blake2b"
)

// V1 Header Structure
// [0]: version int
// [1:4] signature "DtBx"
// [5:36] password
type V1File struct {
	password []byte

	V0File
}

func NewV1File(path string, network *network.DatboxNetwork) *V1File {
	file := new(V1File)
	file.Path = path
	file.network = network
	return file
}

func (f *V1File) Version() int {
	return 1
}

func (f *V1File) OpenOrCreate() error {
	f.octoBuf = make([]byte, 8)
	f.password = make([]byte, 32)
	stat, err := os.Stat(f.Path)
	if err != nil {
		// Not exist
		file, err := os.Create(f.Path)
		if err != nil {
			return err
		}
		f.file = file
		f.writeMode = true
		rand.Read(f.password)
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
		read, err := file.Read(buf)
		if err != nil {
			return err
		}
		if read != 5 {
			return errors.New("Did not read 5 bytes")
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
		read, err = file.Read(f.password)
		if err != nil {
			return err
		}
		if read != 32 {
			return errors.New("Did not read 32 bytes")
		}
		// File size
		read, err = file.Read(f.octoBuf)
		if err != nil {
			file.Close()
			return err
		}
		if read != 8 {
			file.Close()
			return errors.New("Did not read 8 bytes")
		}
		f.size = big.NewInt(0).SetBytes(f.octoBuf).Uint64()
		f.chunks = int(math.Ceil(float64(f.size) / float64(FileChunkSize)))
	}
	return nil
}

func (f *V1File) UploadFrom(path string, channel chan TransferEvent) {
	var err error
	defer func() {
		channel <- TransferEvent{
			Done: true,
			Err:  err,
		}
	}()
	if f.file == nil {
		err = errors.New("No file opened")
		return
	}

	// Write file signature
	f.file.Write([]byte{1})
	f.file.Write([]byte("DtBx"))
	// Write encryption key
	f.file.Write(f.password)
	// Write size
	stat, err := os.Stat(path)
	if err != nil {
		return
	}
	buf := make([]byte, FileChunkSize)
	estimatedChunks := int(math.Ceil(float64(stat.Size()) / FileChunkSize))
	log.Printf("Starting upload of %s\n", path)
	log.Printf("Chunks: %d\n", int(estimatedChunks))

	big.NewInt(stat.Size()).FillBytes(f.octoBuf)
	_, err = f.file.Write(f.octoBuf)
	if err != nil {
		return
	}
	// Piggyback file checksum
	hasher, err := blake2b.New256([]byte("DtBx"))
	if err != nil {
		return
	}

	input, err := os.Open(path)
	if err != nil {
		return
	}
	defer input.Close()
	totalBytes := int64(0)
	for {
		read, err := input.Read(buf)
		if err != nil && err != io.EOF {
			return
		}
		if read == 0 {
			break
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
			return
		}

		// Encrypt data
		block, err := aes.NewCipher(f.password)
		if err != nil {
			return
		}

		padding := aes.BlockSize - len(compressed)%aes.BlockSize
		padBytes := append(compressed, bytes.Repeat([]byte{byte(padding)}, padding)...)

		data := make([]byte, aes.BlockSize+len(padBytes))
		iv := data[:aes.BlockSize]
		if _, err := io.ReadFull(rand.Reader, iv); err != nil {
			return
		}

		mode := cipher.NewCBCEncrypter(block, iv)
		mode.CryptBlocks(data[aes.BlockSize:], padBytes)

		// Send to Discord
		id, err := f.network.SendAttachment(data)
		if err != nil {
			return
		}
		parsed, err := strconv.ParseUint(id, 10, 64)
		f.chunks++
		fmt.Printf("\rUploaded chunks: %d / %d", f.chunks, estimatedChunks)
		big.NewInt(int64(parsed)).FillBytes(f.octoBuf)
		f.file.Write(f.octoBuf)

		totalBytes += int64(read)
		channel <- TransferEvent{
			Current: totalBytes,
			Total:   stat.Size(),
		}
	}
	fmt.Println()

	// Write separator
	big.NewInt(0).FillBytes(f.octoBuf)
	f.file.Write(f.octoBuf)
	// Write file checksum at the end
	f.checksum = hasher.Sum(nil)
	f.file.Write(f.checksum)
	log.Printf("Finished upload of %s\n", path)
	err = nil
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
	block, err := aes.NewCipher(f.password)
	if err != nil {
		return nil, err
	}
	if len(data) < aes.BlockSize {
		return nil, errors.New("Encrypted data is too short")
	}

	iv := data[:aes.BlockSize]
	data = data[aes.BlockSize:]

	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(data, data)

	padding := int(data[len(data)-1])
	data = data[:len(data)-padding]
	// Inflate
	return io.ReadAll(flate.NewReader(bytes.NewReader(data)))
}

func (f *V1File) DownloadTo(path string, channel chan TransferEvent) {
	var err error
	defer func() {
		channel <- TransferEvent{
			Done: true,
			Err:  err,
		}
	}()
	if f.file == nil {
		err = errors.New("No file opened")
		return
	}
	writer, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer writer.Close()

	log.Printf("Starting download of %s", f.Path)

	hasher, err := blake2b.New256([]byte("DtBx"))
	if err != nil {
		return
	}
	estimatedChunks := int(math.Ceil(float64(f.size) / float64(FileChunkSize)))
	chunks := 0
	log.Printf("File has size %d bytes. Chunks: %d", f.Size(), estimatedChunks)

	var data []byte
	totalBytes := 0
	for {
		data, err = f.GetNextChunk()
		if err != nil {
			if err == io.EOF {
				break
			}
			return
		}
		hasher.Write(data)
		writer.Write(data)
		totalBytes += len(data)
		chunks++
		fmt.Printf("\rDownloaded chunks: %d / %d", chunks, estimatedChunks)
		channel <- TransferEvent{
			Current: int64(totalBytes),
			Total:   f.Size(),
		}
	}
	fmt.Println()

	matched, err := f.Verify(hasher.Sum(nil))
	if err != nil {
		return
	}
	if !matched {
		err = errors.New("Downloaded file checksum doesn't match")
		return
	}
	log.Printf("Finished download of %s\n", f.Path)
	err = nil
}
