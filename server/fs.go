package server

import (
	"encoding/json"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type DatboxFileSystem struct {
	dataDir       string
	root          string
	network       *DatboxNetwork
	fileReference map[string]int
}

func NewFileSystem(dataDir, root string, maxJobs int, network *DatboxNetwork) (*DatboxFileSystem, error) {
	fs := new(DatboxFileSystem)
	fs.dataDir = dataDir
	fs.root = root
	fs.network = network
	fs.fileReference = map[string]int{}

	refPath := path.Join(dataDir, "ref.json")
	if _, err := os.Stat(refPath); err == nil {
		// config exists
		file, err := os.Open(refPath)
		if err != nil {
			return nil, err
		}
		defer file.Close()

		bytes, err := io.ReadAll(file)
		if err != nil {
			return nil, err
		}
		json.Unmarshal(bytes, &fs.fileReference)
	}

	return fs, nil
}

func (fs *DatboxFileSystem) sanitize(virtualPath string) string {
	sanitized, err := filepath.Rel(".", virtualPath)
	if err != nil || strings.HasPrefix(sanitized, "..") {
		return "/"
	}
	return sanitized
}
