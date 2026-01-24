package virtualfile

import (
	"datbox/network"
	"fmt"
	"strconv"
)

type Uploader struct {
	buffer          []byte
	bufferLength    int
	chunks          int
	estimatedChunks int
	network         *network.DatboxNetwork
	writeFile       func(id uint64)
}

func (w *Uploader) Write(data []byte) (n int, err error) {
	start := 0
	canRead := min(len(w.buffer)-w.bufferLength, len(data)-start)
	for len(data)-start >= canRead && canRead != 0 {
		copy(w.buffer[w.bufferLength:w.bufferLength+canRead], data[start:start+canRead])
		w.bufferLength += canRead
		start += canRead
		// Buffer is full. Send to Discord
		if w.bufferLength >= len(w.buffer) {
			err := w.Upload()
			if err != nil {
				return 0, err
			}
		}
		canRead = min(len(w.buffer)-w.bufferLength, len(data)-start)
	}
	return len(data), nil
}

func (w *Uploader) Upload() error {
	id, err := w.network.SendAttachment(w.buffer[:w.bufferLength])
	if err != nil {
		return err
	}
	parsed, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return err
	}
	w.writeFile(parsed)
	w.chunks++
	fmt.Printf("\rUploaded chunks: %d / %d", w.chunks, w.estimatedChunks)
	w.bufferLength = 0
	return nil
}
