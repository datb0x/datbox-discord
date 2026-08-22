package main

import (
	"bytes"
	"datbox/server"
	"datbox/server/network"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"path"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"golang.org/x/crypto/blake2b"
)

const (
	RandomSeed = 69420
	ChannelID  = "1526599101272293446"
)

// Generate deterministic byte slice with size
func generateByteSlice(length int) []byte {
	buf := make([]byte, length)
	rand.New(rand.NewSource(RandomSeed)).Read(buf)
	return buf
}

// Create file system
func createFs(tmpDir string) (*server.DatboxFileSystem, error) {
	err := godotenv.Load()
	if err != nil {
		return nil, fmt.Errorf("Failed to load .env file")
	}
	token := os.Getenv("TOKEN")
	if token == "" {
		return nil, fmt.Errorf("Bot token is empty")
	}
	config := server.NewConfig(path.Join(tmpDir, "config.json"), ChannelID, tmpDir, 1, false, token)
	if err := config.Load(); err != nil {
		return nil, fmt.Errorf("Failed to load config: %v", err)
	}
	if err := config.Save(); err != nil {
		return nil, fmt.Errorf("Failed to save config: %v", err)
	}
	network, err := network.NewNetwork(config.Raw.Token, config.Raw.ChannelId, config.Raw.Concurrency)
	if err != nil {
		return nil, fmt.Errorf("Failed to init network: %v", err)
	}
	fs, err := server.NewFileSystem(config, network)
	if err != nil {
		return nil, fmt.Errorf("Failed to init fs: %v", err)
	}
	return fs, nil
}

func testUploadSimple(t *testing.T, fileSize int) {
	tmpDir, err := os.MkdirTemp(os.TempDir(), "datbox-test-*")
	if err != nil {
		t.Errorf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	log.Printf("Created temp dir at %s", tmpDir)
	fs, err := createFs(tmpDir)
	if err != nil {
		t.Error(err)
	}

	fileData := generateByteSlice(fileSize)
	fileHash := blake2b.Sum256(fileData)
	file := bytes.NewReader(fileData)

	message := ""
	lastMessage := ""
	done := false
	go func() {
		for !done {
			if message != lastMessage {
				fmt.Print(message)
				lastMessage = message
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
	_, err = fs.Upload(file, int64(fileSize), "test", "test", 1, &message)
	if err != nil {
		t.Errorf("Failed to upload: %v", err)
	}

	fmt.Println("Uploaded")

	pipeReader, pipeWriter := io.Pipe()
	pipeChannel := make(chan error)

	received := make([]byte, 0)
	go func() {
		buf := make([]byte, fileSize)
		for {
			read, err := pipeReader.Read(buf)
			if err == io.EOF {
				break
			} else if err != nil {
				pipeChannel <- err
				return
			} else {
				received = append(received, buf[:read]...)
			}
		}
		pipeChannel <- nil
	}()

	_, err = fs.Download(pipeWriter, "test", &message)
	done = true
	if err != nil {
		t.Errorf("Failed to download: %v", err)
	}

	fmt.Println("Downloaded")

	newHash := blake2b.Sum256(received)
	for ii := range 32 {
		if fileHash[ii] != newHash[ii] {
			t.Errorf("File checksum mismatch")
		}
	}
}

// TestUploadSimpleSingleChunk uploads and downloads a 1 MB file and compares their checksum
func TestUploadSimpleSingleChunk(t *testing.T) {
	testUploadSimple(t, 1024*1024)
}

// TestUploadSimpleMultiChunk uploads and downloads a 15 MB file and compares their checksum
func TestUploadSimpleMultiChunk(t *testing.T) {
	testUploadSimple(t, 1024*1024*15)
}
