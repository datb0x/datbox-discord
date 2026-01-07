package server

import (
	"compress/gzip"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"math/big"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	cp "github.com/otiai10/copy"
)

const FileChunkSize = 10 * 1024 * 1023

type DatboxFileSystem struct {
	dataDir       string
	root          string
	network       *DatboxNetwork
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
	chunks   int
	checksum []byte
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
	data := make([]byte, 1024)
	var read int
	for read, err = file.Read(data); read > 0 && err == nil; {
		hasher.Write(data[0:read])
	}
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
		data := make([]byte, 8)
		read, err := file.Read(data)
		if err != nil {
			return nil, err
		}
		if read != 8 {
			return nil, errors.New("Did not read 8 bytes")
		}
		size := big.NewInt(0).SetBytes(data).Int64()
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
		stat, err := fs.Stat(virtualPath, len(long) == 0 || !long[1])
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
	stat, err := fs.Stat(path.Join(fs.root, virtualPath), true)
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
				file, err := os.Open(path.Join(fs.root, virtualPath))
				if err != nil {
					return err
				}
				defer file.Close()
				ids := []string{}
				data := make([]byte, 8)
				for read, err := file.Read(data); err != nil && read == 8; {
					id := big.NewInt(0).SetBytes(data).Uint64()
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

type Uploader struct {
	buffer          []byte
	bufferLength    int
	chunks          int
	estimatedChunks int
	network         *DatboxNetwork
	writeFile       func(id uint64)
}

func (w Uploader) Write(data []byte) (n int, err error) {
	for _, b := range data {
		w.buffer[w.bufferLength] = b
		w.bufferLength++
		// Buffer is full. Send to Discord
		if w.bufferLength >= len(w.buffer) {
			id, err := w.network.SendAttachment(w.buffer)
			if err != nil {
				return 0, err
			}
			parsed, err := strconv.ParseUint(id, 10, 64)
			if err != nil {
				return 0, err
			}
			w.writeFile(parsed)
			w.chunks++
			log.Printf("\rUploaded chunks: %d / %d", w.chunks, w.estimatedChunks)
			w.bufferLength = 0
		}
	}
	return len(data), nil
}

func (fs *DatboxFileSystem) Upload(physicalPath, virtualPath string, progressCallback func(current, total int64)) (UploadResult, error) {
	virtualPath = fs.sanitize(virtualPath)
	stat, err := os.Stat(physicalPath)
	if err != nil {
		return UploadResult{}, errors.New("Source file doesn't exist")
	}
	if stat.IsDir() {
		return UploadResult{}, errors.New("Only file uploads are currently supported")
	}

	virtualDir := path.Join(fs.root, path.Dir(virtualPath))
	os.MkdirAll(virtualDir, 0644)
	if _, err = os.Stat(virtualDir); err != nil {
		return UploadResult{}, errors.New("Failed to create directory")
	}
	if _, err = os.Stat(path.Join(fs.root, virtualPath)); err == nil {
		return UploadResult{}, errors.New("File already exists in virtual file system")
	}

	// Actual upload progress
	estimatedChunks := math.Ceil(float64(stat.Size() / FileChunkSize))
	log.Printf("Starting upload of %s\n", physicalPath)
	log.Printf("Estimated chunks (pre-gzip): %d\n", int(estimatedChunks))
	buf := make([]byte, 1024)

	file, err := os.OpenFile(path.Join(fs.root, virtualPath), os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return UploadResult{}, err
	}
	// Write file size
	_, err = file.Write(big.NewInt(stat.Size()).Bytes())
	if err != nil {
		return UploadResult{}, err
	}
	// Piggyback file checksum
	hasher := md5.New()

	reader, err := os.Open(physicalPath)
	if err != nil {
		return UploadResult{}, err
	}
	defer reader.Close()
	uploader := Uploader{
		buffer:          make([]byte, FileChunkSize),
		bufferLength:    0,
		chunks:          0,
		estimatedChunks: int(estimatedChunks),
		network:         fs.network,
		writeFile: func(id uint64) {
			file.Write(big.NewInt(int64(id)).Bytes())
		},
	}
	writer := gzip.NewWriter(io.MultiWriter(uploader, hasher))
	totalBytes := int64(0)
	for read, err := file.Read(buf); err == nil && read > 0; {
		_, err := writer.Write(buf[0:read])
		if err != nil {
			return UploadResult{}, err
		}
		totalBytes += int64(read)
		progressCallback(totalBytes, stat.Size())
	}
	writer.Close()
	log.Println()

	// Write separator
	file.Write(make([]byte, 8))
	// Write file checksum at the end
	checksum := hasher.Sum(nil)
	file.Write(checksum)
	file.Close()

	log.Printf("Finished upload of %s\n", physicalPath)
	return UploadResult{
		Path:     path.Join("/", virtualPath),
		chunks:   uploader.chunks,
		checksum: checksum,
	}, nil
}

type Downloader struct {
	buffer []byte
}

func (d Downloader) Read(buffer []byte) (n int, err error) {
	min := len(buffer)
	if len(d.buffer) < min {
		min = len(d.buffer)
	}
	copy(buffer[0:min], d.buffer[0:min])
	d.buffer = d.buffer[min:]
	return min, nil
}

func (d Downloader) SetBytes(buffer []byte) {
	copy(d.buffer, buffer)
}

func (fs *DatboxFileSystem) Download(virtualPath, physicalPath string, progressCallback func(current, total int64)) error {
	virtualPath = fs.sanitize(virtualPath)
	if _, err := os.Stat(physicalPath); err == nil {
		return errors.New("Physical path " + physicalPath + " already exists. Not overwriting")
	}
	stat, err := os.Stat(virtualPath)
	if err != nil {
		return errors.New("Virtual path " + path.Join(fs.root, virtualPath) + " doesn't exist")
	}

	log.Printf("Starting download of %s", virtualPath)
	file, err := os.Open(path.Join(fs.root, virtualPath))
	if err != nil {
		return err
	}
	defer file.Close()
	writer, err := os.OpenFile(physicalPath, os.O_CREATE|os.O_WRONLY, 0644)
	downloader := Downloader{}
	gunzipper, err := gzip.NewReader(downloader)
	if err != nil {
		return err
	}

	idBuf := make([]byte, 8)
	buffer := make([]byte, 1024)
	read, err := file.Read(idBuf)
	if err != nil {
		return err
	}
	if read != 8 {
		return errors.New("Did not read 8 bytes")
	}

	size := big.NewInt(0).SetBytes(idBuf).Uint64()
	hasher := md5.New()
	estimatedChunks := (stat.Size() - 24) / 8
	chunks := 0
	log.Printf("File has size %d bytes. Estimated chunks (post-gzip): %d", size, estimatedChunks)

	totalBytes := 0

	for read, err = file.Read(idBuf); read == 8 && err == nil; {
		id := big.NewInt(0).SetBytes(idBuf).Uint64()
		if id == 0 {
			break
		}
		data, err := fs.network.FetchAttachment(strconv.FormatUint(id, 10))
		if err != nil {
			return err
		}
		hasher.Write(data)
		downloader.SetBytes(data)
		chunks++
		log.Printf("\rDownloaded chunks: %d / %d", chunks, estimatedChunks)
		for read, err = gunzipper.Read(buffer); read > 0 && err == nil; {
			writer.Write(buffer[:read])
			totalBytes += read
			progressCallback(int64(totalBytes), int64(size))
		}
		if err != nil {
			return err
		}
	}
	log.Println()
	if err != nil {
		return err
	}
	writer.Close()
	gunzipper.Close()

	newChecksum := hasher.Sum(nil)
	oldChecksum := make([]byte, 16)
	read, err = file.Read(oldChecksum)
	if read != 16 || err != nil {
		return errors.New("Virtual file is corrupted")
	}
	for ii := range 16 {
		if newChecksum[ii] != oldChecksum[ii] {
			return errors.New("Downloaded file checksum doesn't match")
		}
	}
	log.Printf("Finished download of %s\n", virtualPath)
	return nil
}
