package virtualfile

import (
	"compress/gzip"
	"datbox/server/network"
	"errors"
	"io"
	"math/big"
	"math/rand"
	"os"
	"strconv"
	"time"
)

type continuousGzipReader struct {
	id       uint64
	reader   *gzip.Reader
	buf      []byte
	lastRead time.Time
	err      error
	wait     bool
	outCh    chan bool
	inCh     chan bool
}

// V0 Header/Footer Structure
// [0:7]: original file size (int64)
// [-24:-17] separator
// [-16:-0] md5 hash
type V0File struct {
	fsData  fsData
	network *network.DatboxNetwork
	file    *os.File

	ptr      int64
	chunks   int
	size     uint64
	octoBuf  []byte
	checksum []byte

	// V0-specific
	targetPtr      int64
	chunkPtr       int64
	chunkBuf       []byte
	lastChunk      int64
	gzipReader     *continuousGzipReader
	gzipPipeWriter *io.PipeWriter
}

func NewV0File(file *os.File, fsData fsData, network *network.DatboxNetwork) (*V0File, error) {
	vFile := new(V0File)
	vFile.fsData = fsData
	vFile.network = network
	vFile.file = file

	stat, err := file.Stat()
	if err != nil && err != os.ErrNotExist {
		return nil, err
	}

	vFile.ptr = 0
	vFile.octoBuf = make([]byte, 8)
	vFile.chunks = max(int(stat.Size()/8)-4, 0)

	read, err := file.Read(vFile.octoBuf)
	if read != 8 || err != nil {
		vFile.size = 0
	} else {
		vFile.size = big.NewInt(0).SetBytes(vFile.octoBuf).Uint64()
	}

	vFile.chunkPtr = 0
	vFile.lastChunk = -1
	return vFile, nil
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

func (f *V0File) Checksum() []byte {
	return f.checksum
}

func (f *V0File) Close() error {
	if f.file != nil {
		return f.file.Close()
	}
	return errors.New("No file opened")
}

func (f *V0File) Write(data []byte) (int, error) {
	return 0, errors.New("Version 0 writing support has been dropped. Please use version 1.")
}

func (f *V0File) Read(buf []byte) (n int, err error) {
	if f.targetPtr >= f.Size() {
		return 0, io.EOF
	} else {
		if f.targetPtr < f.ptr {
			f.chunkPtr = 0
			f.ptr = 0
		}
		// Decode until targetPtr is between lastPtr and ptr
		var data []byte
		lastPtr := f.ptr
		for f.ptr < f.targetPtr {
			data, err = f.ReadChunk(f.chunkPtr)
			if err != nil {
				return
			}
			f.chunkPtr++
			lastPtr = f.ptr
		}
		n = copy(buf, data[f.targetPtr-lastPtr:])
		// Slice can fill buf
		if n >= len(buf) {
			f.targetPtr += int64(n)
			return
		}
		for n < len(buf) {
			data, err = f.ReadChunk(f.chunkPtr)
			if err != nil {
				return
			}
			f.chunkPtr++
			n += copy(buf[n:], data)
		}
		f.targetPtr += int64(n)
		return
	}
}

func (f *V0File) Seek(offset int64, whence int) (int64, error) {
	if whence == io.SeekCurrent {
		f.targetPtr += offset
	} else if whence == io.SeekStart {
		f.targetPtr = offset
	} else if whence == io.SeekEnd {
		f.targetPtr = f.Size() - offset
	}
	return f.targetPtr, nil
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

func (f *V0File) ReadChunk(index int64) ([]byte, error) {
	// The downfall of version 0: If we are not continouously decoding, need to restart reader
	if f.lastChunk == index && f.chunkBuf != nil {
		return f.chunkBuf, nil
	} else if f.lastChunk < index && f.gzipReader != nil && f.gzipPipeWriter != nil {
		// Reading forwards: read all chunks in-between
		// Note: fetching the in-between chunks shouldn't give EOF
		for f.lastChunk < index-1 {
			f.lastChunk++
			raw, err := f.ReadChunkRaw(int64(f.lastChunk))
			if err != nil {
				return nil, err
			}
			f.gzipReader.lastRead = time.Now()
			f.gzipPipeWriter.Write(raw)
		}
		// Wait for gzipReader to finish reading
		for time.Since(f.gzipReader.lastRead) > time.Second && f.gzipReader.err == nil {
			time.Sleep(time.Second)
		}
		if f.gzipReader.err != nil {
			err := f.gzipReader.err
			f.gzipReader = nil
			return nil, err
		}
		// Now we have reached the chunk we want
		eof := false
		raw, err := f.ReadChunkRaw(int64(index))
		if err != nil {
			if err == io.EOF {
				eof = true
			} else {
				return nil, err
			}
		}
		go func() {
			f.gzipPipeWriter.Write(raw)
			if eof {
				f.gzipPipeWriter.Close()
			}
		}()

		f.chunkBuf = make([]byte, 0)
		done := false
		f.gzipReader.wait = true
		for !done {
			select {
			case <-time.After(time.Second):
				done = true
			case <-f.gzipReader.outCh:
				f.ptr += int64(len(f.gzipReader.buf))
				f.chunkBuf = append(f.chunkBuf, f.gzipReader.buf...)
				f.gzipReader.inCh <- true
			}
		}
		return f.chunkBuf, f.gzipReader.err
	} else {
		// Reading backwards: restart
		if f.gzipPipeWriter != nil {
			f.gzipPipeWriter.Close()
		}
		pipeReader, pipeWriter := io.Pipe()
		gzipReader, err := gzip.NewReader(pipeReader)
		if err != nil {
			return nil, err
		}
		// Keep a local copy for the goroutine
		contReader := continuousGzipReader{
			id:       rand.Uint64(),
			reader:   gzipReader,
			buf:      make([]byte, FileChunkSize/1024),
			lastRead: time.Now(),
			err:      nil,
			wait:     false,
		}
		f.gzipReader = &contReader
		go func() {
			var err error
			for contReader.id == f.gzipReader.id && err == nil {
				_, err = gzipReader.Read(contReader.buf)
				contReader.lastRead = time.Now()
				if contReader.wait {
					contReader.outCh <- true
					if !<-contReader.inCh {
						break
					}
				}
			}
			if contReader.id == f.gzipReader.id {
				f.gzipReader.err = err
			}
		}()
		f.gzipPipeWriter = pipeWriter
		f.lastChunk = -1
		f.ptr = 0
		f.chunkBuf = nil
		return f.ReadChunk(index)
	}
}

func (f *V0File) ReadChunkRaw(index int64) ([]byte, error) {
	_, err := f.file.Seek(8*index, io.SeekStart)
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

func (f *V0File) WriteChunk(index int64, data []byte) (uint64, error) {
	return 0, errors.New("Version 0 writing support has been dropped. Please use version 1.")
}

func (f *V0File) Raw() *os.File {
	return f.file
}
