package virtualfile

import (
	"bufio"
	"compress/gzip"
	"crypto/md5"
	"datbox/network"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
)

type V0File struct {
	Path           string
	RelPath        string
	FileSystemMsg  string
	FileSystemHash string
	writeMode      bool
	network        *network.DatboxNetwork
	file           *os.File
	chunks         int
	size           uint64
	octoBuf        []byte
	checksum       []byte

	// V0-specific
	lastChunk  int
	gzipReader *gzip.Reader
}

func NewV0File(root, path, fsMsg, fsHash string, network *network.DatboxNetwork) *V0File {
	file := new(V0File)
	file.Path = filepath.Join(root, path)
	file.RelPath = path
	file.FileSystemMsg = fsMsg
	file.FileSystemHash = fsHash
	file.network = network
	file.lastChunk = -1
	return file
}

func (f *V0File) Version() int {
	return 0
}

func (f *V0File) Chunks() int {
	return f.chunks
}

func (f *V0File) Size() int64 {
	return int64(f.size)
}

func (f *V0File) WriteMode() bool {
	return f.writeMode
}

func (f *V0File) Checksum() []byte {
	return f.checksum
}

func (f *V0File) OpenOrCreate() error {
	f.octoBuf = make([]byte, 8)
	stat, err := os.Stat(f.Path)
	if err != nil {
		// Not exist
		file, err := os.Create(f.Path)
		if err != nil {
			return err
		}
		f.file = file
		f.writeMode = true
	} else {
		// Exists
		file, err := os.Open(f.Path)
		if err != nil {
			return err
		}
		f.file = file
		f.writeMode = false
		if (stat.Size() % 8) != 0 {
			return errors.New("File is not version 0")
		}
		f.chunks = int((stat.Size() - 32) / 8)
		// Read header
		_, err = io.ReadFull(file, f.octoBuf)
		if err != nil {
			file.Close()
			return err
		}
		f.size = big.NewInt(0).SetBytes(f.octoBuf).Uint64()
	}
	return nil
}

func (f *V0File) Close() error {
	if f.file != nil {
		return f.file.Close()
	}
	return errors.New("No file opened")
}

func (f *V0File) Write(data []byte) error {
	written, err := f.file.Write(data)
	if err != nil {
		return err
	}
	if written != len(data) {
		return fmt.Errorf("Did not write %d bytes", len(data))
	}
	return nil
}

func (f *V0File) CopyHeader(header *network.DatboxHeader) error {
	return nil
}

func (f *V0File) WriteHeader() error {
	// Write file size
	big.NewInt(int64(f.size)).FillBytes(f.octoBuf)
	_, err := f.file.Write(f.octoBuf)
	if err != nil {
		return err
	}
	return nil
}

func (f *V0File) WriteMsgID(id uint64) error {
	if f.file == nil {
		return errors.New("No file opened")
	}

	big.NewInt(int64(id)).FillBytes(f.octoBuf)
	wrote, err := f.file.Write(f.octoBuf)
	if err != nil {
		return err
	}
	if wrote != 8 {
		return errors.New("Did not write 8 bytes")
	}
	return nil
}

func (f *V0File) Upload(fileReader io.Reader, size int64, channel chan TransferEvent) {
	if f.file == nil {
		endTransfer(channel, errors.New("No file opened"))
		return
	}

	buf := make([]byte, 4096)
	f.size = uint64(size)

	// Begin header
	header := network.NewHeader(network.ActionBegin)
	header.Fields["fs-msg"] = f.FileSystemMsg
	header.Fields["fs-hash"] = f.FileSystemHash
	header.Fields["path"] = f.RelPath
	header.Fields["version"] = "0"
	header.Fields["size"] = fmt.Sprint(f.size)
	err := f.network.SendMessage(header)
	if err != nil {
		endTransfer(channel, err)
		return
	}

	// Chunk header
	header.Action = network.ActionFileChunk
	delete(header.Fields, "size")

	estimatedChunks := int(math.Ceil(float64(size) / FileChunkSize))
	uploadId := randomId()
	log.Printf("(%s) Chunks (pre-gzip): %d\n", uploadId, int(estimatedChunks))

	// Piggyback file checksum
	hasher := md5.New()

	pipeReader, pipeWriter := io.Pipe()
	readerSignal := make(chan error)
	go func() {
		bufReader := bufio.NewReaderSize(pipeReader, FileChunkSize)
		chunkBuf := make([]byte, FileChunkSize)
		index := 0

		upload := func(length int) error {
			header.Fields["index"] = fmt.Sprint(index)
			index++
			id, err := f.network.SendAttachment(chunkBuf[:length], header.String())
			if err != nil {
				return err
			}
			parsed, err := strconv.ParseUint(id, 10, 64)
			if err != nil {
				return err
			}
			big.NewInt(int64(parsed)).FillBytes(f.octoBuf)
			f.file.Write(f.octoBuf)
			f.chunks++
			fmt.Printf("\r(%s) Uploaded chunks: %d / %d", uploadId, f.chunks, estimatedChunks)
			return nil
		}

		for {
			read, err := ReadFill(bufReader, chunkBuf)
			if err != nil && err != io.EOF {
				readerSignal <- err
				return
			}

			err = upload(read)
			if err != nil {
				readerSignal <- err
				return
			}
			if read != len(chunkBuf) {
				break
			}
		}
	}()

	gzipWriter := gzip.NewWriter(io.MultiWriter(pipeWriter, hasher))
	totalBytes := int64(0)
	for {
		read, err := fileReader.Read(buf)
		if err != nil && err != io.EOF {
			endTransfer(channel, err)
			return
		}
		if read == 0 {
			break
		}
		select {
		case err, ok := <-readerSignal:
			if err != nil && ok {
				endTransfer(channel, err)
				return
			}
		default:
		}
		_, err = gzipWriter.Write(buf[:read])
		if err != nil {
			endTransfer(channel, err)
			return
		}
		totalBytes += int64(read)
		channel <- TransferEvent{
			Current: totalBytes,
			Total:   size,
		}
	}
	gzipWriter.Close()
	pipeWriter.Close()
	channel <- TransferEvent{
		Current: totalBytes,
		Total:   size,
	}
	err = <-readerSignal
	if err != nil {
		endTransfer(channel, err)
		return
	}
	fmt.Println()

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
	err = f.network.SendMessage(header)
	endTransfer(channel, err)
}

func (f *V0File) ReadMsgID() (uint64, error) {
	if f.file == nil {
		return 0, errors.New("No file opened")
	}
	_, err := io.ReadFull(f.file, f.octoBuf)
	if err != nil {
		return 0, err
	}
	return big.NewInt(0).SetBytes(f.octoBuf).Uint64(), nil
}

func (f *V0File) ReadPrevMsgID() (uint64, error) {
	if f.file == nil {
		return 0, errors.New("No file opened")
	}
	_, err := f.file.Seek(-16, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	return f.ReadMsgID()
}

func (f *V0File) Verify(checksum []byte) (bool, error) {
	if f.file == nil {
		return false, errors.New("No file opened")
	}
	buf := make([]byte, len(checksum))
	f.file.Seek(-16, io.SeekEnd)
	read, err := f.file.Read(buf)
	if read != 16 || err != nil && err != io.EOF {
		return false, errors.New("Virtual file is corrupted")
	}
	for ii := range 16 {
		if buf[ii] != checksum[ii] {
			return false, nil
		}
	}
	return true, nil
}

func (f *V0File) GetNextChunk() ([]byte, error) {
	return nil, errors.New("Version 0 files cannot be decompressed by chunks")
}

func (f *V0File) GetNextChunkRaw() ([]byte, error) {
	id, err := f.ReadMsgID()
	if err != nil {
		// Rewind on error
		f.file.Seek(-8, io.SeekCurrent)
		return nil, err
	}
	if id == 0 {
		return nil, io.EOF
	}
	return f.network.FetchAttachment(strconv.FormatUint(id, 10))
}

func (f *V0File) Download(fileWriter io.WriteCloser, channel chan TransferEvent) {
	if f.file == nil {
		endTransfer(channel, errors.New("No file opened"))
		return
	}
	defer fileWriter.Close()
	stat, err := os.Stat(f.Path)
	if err != nil {
		endTransfer(channel, err)
		return
	}

	hasher := md5.New()
	estimatedChunks := (stat.Size() - 32) / 8
	chunks := 0

	pipeReader, pipeWriter := io.Pipe()

	// Put gzip decompressor in goroutine
	gzipSignal := make(chan error)
	totalBytes := 0
	go func() {
		buf := make([]byte, 4096)
		gzipReader, err := gzip.NewReader(bufio.NewReaderSize(pipeReader, FileChunkSize))
		if err != nil {
			gzipSignal <- err
			return
		}
		defer gzipReader.Close()
		for {
			read, err := gzipReader.Read(buf)
			if err != nil && err != io.EOF {
				gzipSignal <- err
				return
			}
			if read == 0 {
				break
			}
			totalBytes += read
			channel <- TransferEvent{
				Current: int64(totalBytes),
				Total:   f.Size(),
			}
			_, err = fileWriter.Write(buf[:read])
			if err != nil {
				gzipSignal <- err
				return
			}
		}
		channel <- TransferEvent{
			Current: int64(totalBytes),
			Total:   f.Size(),
		}
		gzipSignal <- nil
	}()

	var data []byte
	var ok bool
	for {
		data, err = f.GetNextChunkRaw()
		if err != nil {
			if err == io.EOF {
				break
			}
			endTransfer(channel, err)
			return
		}
		hasher.Write(data)
		// Check if there's error in gzip
		select {
		case err, ok = <-gzipSignal:
			if ok && err != nil {
				endTransfer(channel, err)
				return
			}
		default:
		}
		_, err = pipeWriter.Write(data)
		if err != nil {
			endTransfer(channel, err)
			return
		}
		chunks++
		fmt.Printf("\r[%s] %d/%d chunks, %d/%d bytes", f.RelPath, chunks, estimatedChunks, totalBytes, f.size)
	}
	fmt.Println()
	pipeWriter.Close()
	// Wait for gzip to be done
	err = <-gzipSignal
	if err != nil {
		endTransfer(channel, err)
		return
	}

	newChecksum := hasher.Sum(nil)
	matched, err := f.Verify(newChecksum)
	if err != nil {
		endTransfer(channel, err)
		return
	}
	if !matched {
		endTransfer(channel, errors.New("Downloaded file checksum doesn't match"))
		return
	}
	endTransfer(channel, nil)
}
