package server

import (
	"crypto/md5"
	"datbox/network"
	"datbox/virtualfile"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	cp "github.com/otiai10/copy"
)

type DatboxFileSystem struct {
	dataDir       string
	root          string
	network       *network.DatboxNetwork
	fileReference map[string]int
}

type FileInfo struct {
	stat os.FileInfo
	size int64
}

func (info FileInfo) Name() string       { return info.stat.Name() }
func (info FileInfo) Size() int64        { return info.size }
func (info FileInfo) Mode() os.FileMode  { return info.stat.Mode() }
func (info FileInfo) ModTime() time.Time { return info.stat.ModTime() }
func (info FileInfo) IsDir() bool        { return info.stat.IsDir() }
func (info FileInfo) Sys() any           { return info.stat.Sys() }

type DirEntry struct {
	Name string
	Stat os.FileInfo
}

type UploadResult struct {
	Path     string
	Chunks   int
	Checksum []byte
}

func NewFileSystem(dataDir string, maxJobs int, network *network.DatboxNetwork) (*DatboxFileSystem, error) {
	fs := new(DatboxFileSystem)
	fs.dataDir = dataDir
	fs.root = path.Join(dataDir, "root")
	fs.network = network
	fs.fileReference = map[string]int{}

	os.MkdirAll(fs.root, 0755)

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
	sanitized, err := filepath.Rel(fs.root, path.Join(fs.root, virtualPath))
	if err != nil || strings.HasPrefix(sanitized, "..") {
		return "/"
	}
	return sanitized
}

func (fs *DatboxFileSystem) md5(physicalPath string) (string, error) {
	stat, err := os.Stat(physicalPath)
	if err != nil {
		return "", err
	}
	if stat.IsDir() {
		return "", errors.New("MD5 checksum can only be done on files")
	}
	hasher := md5.New()
	file, err := os.Open(physicalPath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}
	hasher.Write(data)
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func (fs *DatboxFileSystem) saveReference() error {
	bytes, err := json.MarshalIndent(fs.fileReference, "", "\t")
	if err != nil {
		return err
	}
	return os.WriteFile(path.Join(fs.dataDir, "ref.json"), bytes, 0644)
}

func (fs *DatboxFileSystem) exists(virtualPath string) bool {
	_, err := os.Stat(path.Join(fs.root, virtualPath))
	return err == nil
}

func (fs *DatboxFileSystem) Mkdir(virtualPath string, all ...bool) error {
	virtualPath = fs.sanitize(virtualPath)
	if len(all) == 1 && all[0] {
		return os.MkdirAll(virtualPath, 0644)
	} else {
		return os.Mkdir(virtualPath, 0644)
	}
}

func (fs *DatboxFileSystem) Stat(virtualPath string, local ...bool) (os.FileInfo, error) {
	virtualPath = fs.sanitize(virtualPath)
	stat, err := os.Stat(path.Join(fs.root, virtualPath))
	if err != nil {
		return nil, err
	}
	if !stat.IsDir() && (len(local) == 0 || !local[0]) {
		file, err := os.Open(path.Join(fs.root, virtualPath))
		if err != nil {
			return nil, err
		}
		defer file.Close()
		var size int64
		if stat.Size()%8 == 0 {
			// Version 0
			data := make([]byte, 8)
			read, err := file.Read(data)
			if err != nil {
				return nil, err
			}
			if read != 8 {
				return nil, errors.New("Did not read 8 bytes")
			}
			size = big.NewInt(0).SetBytes(data).Int64()
		} else {
			// Version 1+
			data := make([]byte, 5)
			read, err := file.Read(data)
			if err != nil {
				return nil, err
			}
			if read != 5 {
				return nil, errors.New("Did not read 5 bytes")
			}
			if string(data[1:]) != "DtBx" {
				return nil, errors.New("Wrong file signature")
			}
			file.Seek(37, io.SeekStart)
			data = make([]byte, 8)
			read, err = file.Read(data)
			if err != nil {
				return nil, err
			}
			if read != 8 {
				return nil, errors.New("Did not read 8 bytes")
			}
			size = big.NewInt(0).SetBytes(data).Int64()
		}
		newStat := FileInfo{stat: stat, size: size}
		return newStat, nil
	}
	return stat, nil
}

func (fs *DatboxFileSystem) ReadDir(virtualPath string, long ...bool) ([]DirEntry, error) {
	virtualPath = fs.sanitize(virtualPath)
	if stat, err := os.Stat(path.Join(fs.root, virtualPath)); err != nil || !stat.IsDir() {
		if err != nil {
			return nil, err
		}
		return nil, errors.New("Not a directory")
	}
	results := []DirEntry{}
	entries, err := os.ReadDir(path.Join(fs.root, virtualPath))
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		stat, err := fs.Stat(path.Join(virtualPath, entry.Name()), len(long) == 0 || !long[0])
		if err != nil {
			return nil, err
		}
		results = append(results, DirEntry{Name: entry.Name(), Stat: stat})
	}
	return results, nil
}

func (fs *DatboxFileSystem) Move(src, dest string) error {
	src = fs.sanitize(src)
	dest = fs.sanitize(dest)
	if !fs.exists(src) {
		return errors.New("Source file doesn't exist")
	}
	if !fs.exists(dest) {
		return errors.New("Destination file doesn't exist")
	}

	return os.Rename(path.Join(fs.root, src), path.Join(fs.root, dest))
}

func (fs *DatboxFileSystem) recursiveIncrementReference(eitherPath string) error {
	stat, err := os.Stat(eitherPath)
	if err != nil {
		return err
	}
	if stat.IsDir() {
		entries, err := os.ReadDir(eitherPath)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			err := fs.recursiveIncrementReference(path.Join(eitherPath, entry.Name()))
			if err != nil {
				return err
			}
		}
	} else {
		hash, err := fs.md5(eitherPath)
		if err != nil {
			return err
		}
		value, exists := fs.fileReference[hash]
		if exists {
			fs.fileReference[hash] = value + 1
		} else {
			fs.fileReference[hash] = 1
		}
	}
	return nil
}

func (fs *DatboxFileSystem) Copy(src, dest string) error {
	src = fs.sanitize(src)
	dest = fs.sanitize(dest)
	err := cp.Copy(path.Join(fs.root, src), path.Join(fs.root, dest))
	if err != nil {
		return err
	}
	err = fs.recursiveIncrementReference(path.Join(fs.root, src))
	if err != nil {
		return err
	}
	return fs.saveReference()
}

func (fs *DatboxFileSystem) Remove(virtualPath string, options ...bool) error {
	virtualPath = fs.sanitize(virtualPath)
	recursive := len(options) > 0 && options[0]
	remote := len(options) > 1 && options[1]
	if !fs.exists(virtualPath) {
		return errors.New("File doesn't exist")
	}
	stat, err := fs.Stat(virtualPath, true)
	if err != nil {
		return err
	}
	if stat.IsDir() {
		if !recursive {
			return errors.New("Cannot remove directory. Consider setting recursive to true")
		}
		entries, err := os.ReadDir(path.Join(fs.root, virtualPath))
		if err != nil {
			return err
		}
		for _, entry := range entries {
			err = fs.Remove(path.Join(virtualPath, entry.Name()), recursive, remote)
			if err != nil {
				return err
			}
		}
	} else {
		hash, err := fs.md5(path.Join(fs.root, virtualPath))
		if err != nil {
			return err
		}
		refs, exists := fs.fileReference[hash]
		if !exists {
			refs = 0
		} else {
			refs -= 1
		}
		if remote {
			if refs >= 1 {
				log.Println("Cannot delete remote. Another file referencing the same chunks exist")
			} else {
				file, err := virtualfile.OpenVirtualFile(path.Join(fs.root, virtualPath), fs.network)
				if err != nil {
					return err
				}
				err = file.OpenOrCreate()
				if err != nil {
					return err
				}
				defer file.Close()
				ids := []string{}
				for {
					id, err := file.ReadMsgID()
					if err != nil {
						return err
					}
					if id == 0 {
						break
					}
					ids = append(ids, strconv.FormatUint(id, 10))
				}
				fs.network.DeleteMessages(ids)
			}
		}
		err = os.Remove(path.Join(fs.root, virtualPath))
		if err != nil {
			return err
		}
		if refs > 0 {
			fs.fileReference[hash] = refs
		} else {
			delete(fs.fileReference, hash)
		}
	}
	return fs.saveReference()
}

func (fs *DatboxFileSystem) Upload(physicalPath, virtualPath string, fileVersion byte, logger chan []byte) error {
	virtualPath = fs.sanitize(virtualPath)
	stat, err := os.Stat(physicalPath)
	if err != nil {
		return errors.New("Source file doesn't exist")
	}
	if stat.IsDir() {
		return errors.New("Only file uploads are currently supported")
	}

	virtualDir := path.Join(fs.root, path.Dir(virtualPath))
	os.MkdirAll(virtualDir, 0644)
	if _, err = os.Stat(virtualDir); err != nil {
		return errors.New("Failed to create directory")
	}
	if _, err = os.Stat(path.Join(fs.root, virtualPath)); err == nil {
		return errors.New("File already exists in virtual file system")
	}

	file, err := virtualfile.CreateVirtualFile(path.Join(fs.root, virtualPath), fs.network, fileVersion)
	if err != nil {
		return err
	}
	err = file.OpenOrCreate()
	if err != nil {
		return err
	}
	if !file.WriteMode() {
		return errors.New("File should be in write mode")
	}

	eventSignal := make(chan virtualfile.TransferEvent)
	go file.UploadFrom(physicalPath, eventSignal)
	for {
		event := <-eventSignal
		if event.Done {
			if event.Err != nil {
				return event.Err
			}
			str := fmt.Sprintf("\rProgress: 100%% (%d / %d)", file.Size(), file.Size())
			logger <- append([]byte{2}, []byte(str)...)
			break
		} else {
			str := fmt.Sprintf("\rProgress: %03d%% (%d / %d)", int(100*event.Current/event.Total), event.Current, event.Total)
			logger <- append([]byte{3}, []byte(str)...)
		}
	}

	str := fmt.Sprintf("\nUploaded to %s as %d chunks (MD5 %s)", path.Join("/", virtualPath), file.Chunks(), hex.EncodeToString(file.Checksum()))
	logger <- append([]byte{0}, []byte(str)...)

	return nil
}

func (fs *DatboxFileSystem) Download(virtualPath, physicalPath string, logger chan []byte) error {
	virtualPath = fs.sanitize(virtualPath)
	if _, err := os.Stat(physicalPath); err == nil {
		return errors.New("Physical path " + physicalPath + " already exists. Not overwriting")
	}
	_, err := fs.Stat(virtualPath, true)
	if err != nil {
		return errors.New("Virtual path " + path.Join(fs.root, virtualPath) + " doesn't exist")
	}

	file, err := virtualfile.OpenVirtualFile(path.Join(fs.root, virtualPath), fs.network)
	if err != nil {
		return err
	}
	defer file.Close()
	err = file.OpenOrCreate()
	if err != nil {
		return err
	}
	if file.WriteMode() {
		return errors.New("File should not be in write mode")
	}
	eventSignal := make(chan virtualfile.TransferEvent)
	go file.DownloadTo(physicalPath, eventSignal)
	for {
		event := <-eventSignal
		if event.Done {
			if event.Err != nil {
				return event.Err
			}
			str := fmt.Sprintf("\rProgress: 100%% (%d / %d)", file.Size(), file.Size())
			logger <- append([]byte{2}, []byte(str)...)
			break
		} else {
			str := fmt.Sprintf("\rProgress: %03d%% (%d / %d)", int(100*event.Current/event.Total), event.Current, event.Total)
			logger <- append([]byte{3}, []byte(str)...)
		}
	}

	str := fmt.Sprintf("\nDownloaded to %s successfully", physicalPath)
	logger <- append([]byte{0}, []byte(str)...)

	return nil
}
