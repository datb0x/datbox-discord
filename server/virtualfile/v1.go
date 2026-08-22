package virtualfile

import (
	"bytes"
	"compress/flate"
	"crypto/rand"
	"datbox/server/network"
	"datbox/util/structs"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"os"
	"strconv"
)

// V1 Header Structure
// [0]: version int
// [1:4] signature "DtBx"
// [5:36] password
// [37:44] file size
// [-24:-17] separator
// [-16:-0] md5 hash
type V1File struct {
	password []byte

	V0File
}

func NewV1File(file *os.File, fsData fsData, network *network.DatboxNetwork) (*V1File, error) {
	vFile := new(V1File)
	vFile.fsData = fsData
	vFile.network = network
	vFile.file = file

	stat, err := file.Stat()
	if err != nil && err != os.ErrNotExist {
		return nil, err
	}

	vFile.ptr = 0
	vFile.octoBuf = make([]byte, 8)
	vFile.password = make([]byte, 32)

	if err == os.ErrNotExist || stat.Size() == 0 {
		// Generate password
		_, err = rand.Read(vFile.password)
		if err != nil {
			return nil, err
		}
		// Write file signature
		file.Write([]byte{1})
		file.Write([]byte("DtBx"))
		// Write encryption key
		file.Write(vFile.password)
		// Initialize file
		vFile.chunks = 0
		vFile.size = 0
	} else {
		if (stat.Size() % 8) == 0 {
			return nil, errors.New("File is not version 1")
		}
		// Read header
		_, err = io.ReadFull(file, vFile.octoBuf[:5])
		if err != nil {
			return nil, err
		}
		// Version number
		if vFile.octoBuf[0] != 1 {
			return nil, errors.New("File is not version 1")
		}
		// File signature
		if string(vFile.octoBuf[1:5]) != "DtBx" {
			return nil, errors.New("Wrong file signature")
		}
		// File encryption key
		_, err = io.ReadFull(file, vFile.password)
		if err != nil {
			return nil, err
		}
		// File size
		_, err = io.ReadFull(file, vFile.octoBuf)
		if err != nil {
			file.Close()
			return nil, err
		}
		vFile.size = big.NewInt(0).SetBytes(vFile.octoBuf).Uint64()
		vFile.chunks = int(math.Ceil(float64(vFile.size) / float64(FileChunkSize)))
	}

	return vFile, nil
}

func (f *V1File) Version() int {
	return 1
}

func (f *V1File) CopyHeader(header *network.DatboxHeader) (err error) {
	f.password, err = hex.DecodeString(header.Fields["password"])
	if err != nil {
		return
	}
	if f.fsData.Password != nil {
		f.password, err = SymmetricDecrypt(f.fsData.Password, f.password)
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
	f.file.Write(f.password)
	// Write size
	big.NewInt(int64(f.size)).FillBytes(f.octoBuf)
	_, err := f.file.Write(f.octoBuf)
	if err != nil {
		return err
	}
	return nil
}

func (f *V1File) Write(data []byte) (n int, err error) {
	// Concurrency upload queue
	futures := structs.NewQueue[structs.Future[uint64]](f.network.Uploader.Concurrency)
	dequeueFuture := func() error {
		future := futures.Dequeue()
		_, err := future.Await()
		return err
	}

	for n < len(data) {
		chunkIndex := f.ptr / FileChunkSize
		chunkOffset := f.ptr - chunkIndex*FileChunkSize

		var buf []byte
		buf, err = f.ReadChunk(chunkIndex)
		if err == io.EOF {
			// Need new chunk
			buf = data[n:min(n+FileChunkSize, len(data))]
			n += len(buf)
			f.size += uint64(len(buf))
			f.chunks++
		} else if err != nil {
			return
		} else {
			copied := copy(buf[chunkOffset:], data[n:])
			f.ptr += int64(copied)
			n += copied
		}

		// Send to Discord
		for futures.IsFull() {
			dequeueFuture()
		}
		futures.Enqueue(structs.NewFuture(func() (uint64, error) {
			return f.WriteChunk(chunkIndex, buf)
		}), false)
	}

	for !futures.IsEmpty() {
		dequeueFuture()
	}

	// Update size in header
	big.NewInt(int64(f.size)).FillBytes(f.octoBuf)
	_, err = f.file.Seek(37, io.SeekStart)
	if err != nil {
		return
	}
	f.file.Write(f.octoBuf)

	// Write separator and empty checksum
	big.NewInt(0).FillBytes(f.octoBuf)
	_, err = f.file.Seek(45+int64(f.chunks)*8, io.SeekStart)
	if err != nil {
		return
	}
	for range 5 {
		f.file.Write(f.octoBuf)
	}

	return
}

func (f *V1File) Read(buf []byte) (n int, err error) {
	var data []byte
	chunkIndex := f.ptr / FileChunkSize
	for n < len(buf) {
		data, err = f.ReadChunk(chunkIndex)
		if err != nil {
			if err == io.EOF {
				err = nil
				break
			}
			return
		}
		n += copy(buf[n:], data)
	}
	f.ptr += int64(n)
	return
}

func (f *V1File) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		f.ptr = offset
	case io.SeekCurrent:
		f.ptr += offset
	case io.SeekEnd:
		f.ptr = f.Size() - offset
	}
	return f.ptr, nil
}

func (f *V1File) ReadChunk(index int64) ([]byte, error) {
	raw, err := f.ReadChunkRaw(index)
	if err != nil {
		return nil, err
	}

	// Decrypt
	raw, err = SymmetricDecrypt(f.password, raw)
	// Inflate
	return io.ReadAll(flate.NewReader(bytes.NewReader(raw)))
}

func (f *V1File) ReadChunkRaw(index int64) ([]byte, error) {
	_, err := f.file.Seek(37+8*index, io.SeekStart)
	if err != nil {
		return nil, err
	}
	id, err := f.ReadMsgID()
	if err != nil {
		return nil, err
	}
	if id == 0 {
		return nil, io.EOF
	}
	return f.network.FetchAttachment(strconv.FormatUint(id, 10))
}

func (f *V1File) WriteChunk(index int64, data []byte) (uint64, error) {
	// Setup header
	header := network.NewHeader(network.ActionFileChunk)
	header.Fields["fs-msg"] = f.fsData.MsgID
	header.Fields["fs-hash"] = f.fsData.Hash
	header.Fields["path"] = f.fsData.RelPath
	header.Fields["version"] = "1"
	header.Fields["index"] = fmt.Sprint(index)

	// Deflate data
	pipeReader, pipeWriter := io.Pipe()
	go func() {
		flateWriter, err := flate.NewWriter(pipeWriter, flate.DefaultCompression)
		if err != nil {
			flateWriter.Close()
		}
		flateWriter.Write(data)
		flateWriter.Close()
		pipeWriter.Close()
	}()
	compressed, err := io.ReadAll(pipeReader)
	if err != nil {
		return 0, err
	}

	// Encrypt data
	encrpyted, err := SymmetricEncrypt(f.password, compressed)
	if err != nil {
		return 0, err
	}

	// Send and get message ID
	id, err := f.network.Uploader.SendAttachment(encrpyted, header.String(), false)
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return 0, err
	}

	// Write to file
	_, err = f.file.Seek(45+index*8, io.SeekStart)
	if err != nil {
		return 0, err
	}
	big.NewInt(int64(parsed)).FillBytes(f.octoBuf)
	_, err = f.file.Write(f.octoBuf)
	return parsed, err
}
