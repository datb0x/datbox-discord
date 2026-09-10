package datboxdiscord

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"math/big"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/datb0x/datbox-discord/internal"
	"github.com/datb0x/datbox-discord/network"
	"github.com/datb0x/datbox-discord/virtualfile"

	"github.com/dustin/go-humanize"
	cp "github.com/otiai10/copy"
	"golang.org/x/crypto/blake2b"
	"golang.org/x/term"
)

type discordFileSystem struct {
	config         *discordConfig
	root           string
	network        *network.DiscordNetwork
	fileReference  map[string]int
	initialized    bool
	lastFsHash     string
	lastFsMsgID    string
	globalPassword []byte
	dirty          bool
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

func newFileSystem(config *discordConfig, network *network.DiscordNetwork) (*discordFileSystem, error) {
	fs := new(discordFileSystem)
	fs.config = config
	fs.root = path.Join(fs.config.DataDir, "root")
	fs.network = network
	fs.fileReference = map[string]int{}
	fs.initialized = false

	if fs.config.Password != "" && fs.config.Password != "skip" {
		var err error
		fs.globalPassword, err = hex.DecodeString(fs.config.Password)
		if err != nil {
			return nil, err
		}
	}

	os.MkdirAll(fs.root, 0755)

	refPath := path.Join(fs.config.DataDir, "ref.json")
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

	err := fs.syncIfNeeded()
	if err != nil {
		return nil, err
	}

	fs.initialized = true
	return fs, nil
}

func (fs *discordFileSystem) sanitize(virtualPath string) string {
	sanitized, err := filepath.Rel(fs.root, fs.TranslatePath(virtualPath))
	if err != nil || strings.HasPrefix(sanitized, "..") {
		return "/"
	}
	return sanitized
}

func (fs *discordFileSystem) md5(physicalPath string) (string, error) {
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

func (fs *discordFileSystem) saveReference() error {
	bytes, err := json.MarshalIndent(fs.fileReference, "", "\t")
	if err != nil {
		return err
	}
	return os.WriteFile(path.Join(fs.config.DataDir, "ref.json"), bytes, 0644)
}

func (fs *discordFileSystem) exists(virtualPath string) bool {
	_, err := os.Stat(fs.TranslatePath(virtualPath))
	return err == nil
}

func (dbfs *discordFileSystem) packFileSystem(hashOnly bool) ([]byte, string, error) {
	var data bytes.Buffer
	hasher, err := blake2b.New256([]byte("DtBx"))
	if err != nil {
		return nil, "", err
	}
	var writer io.Writer
	if hashOnly {
		writer = hasher
	} else {
		writer = io.MultiWriter(&data, hasher)
	}
	octoBuf := make([]byte, 8)
	err = filepath.Walk(dbfs.root, func(path string, info fs.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}
		file, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		path, err = filepath.Rel(dbfs.root, path)
		if err != nil {
			return err
		}
		binary.BigEndian.PutUint64(octoBuf, uint64(len(path)))
		writer.Write(octoBuf)
		writer.Write([]byte(path))
		binary.BigEndian.PutUint64(octoBuf, uint64(info.Size()))
		writer.Write(octoBuf)
		writer.Write(file)
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	return data.Bytes(), hex.EncodeToString(hasher.Sum(nil)), nil
}

func (dbfs *discordFileSystem) mergeFileSystem(remoteFs []byte) error {
	octoBuf := make([]byte, 8)
	reader := bytes.NewReader(remoteFs)
	for {
		read, err := reader.Read(octoBuf)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		if read != 8 {
			return io.ErrUnexpectedEOF
		}
		length := big.NewInt(0).SetBytes(octoBuf).Uint64()
		buf := make([]byte, length)
		_, err = io.ReadFull(reader, buf)
		if err != nil {
			return err
		}
		path := string(buf)
		_, err = io.ReadFull(reader, octoBuf)
		if err != nil {
			return err
		}
		length = big.NewInt(0).SetBytes(octoBuf).Uint64()
		buf = make([]byte, length)
		_, err = io.ReadFull(reader, buf)
		if err != nil {
			return err
		}
		if dbfs.exists(path) {
			localData, err := os.ReadFile(filepath.Join(dbfs.root, path))
			if err != nil {
				return err
			}
			localHasher := md5.New()
			remoteHasher := md5.New()
			localHasher.Write(localData)
			remoteHasher.Write(buf)
			if bytes.Equal(localHasher.Sum(nil), remoteHasher.Sum(nil)) {
				continue
			}
			internal.Logger.Printf("%s already exists locally. Overwrite with remote version? [y/n]", path)
			var ans string
			fmt.Scanf("%s", &ans)
			if strings.ToLower(ans) != "y" {
				continue
			}
		}
		err = os.MkdirAll(filepath.Dir(filepath.Join(dbfs.root, path)), 0755)
		if err != nil {
			return err
		}
		err = os.WriteFile(filepath.Join(dbfs.root, path), buf, 0644)
		if err != nil {
			return err
		}
	}
	internal.Logger.Debug("Merged remote filesystem")
	return nil
}

func (fs *discordFileSystem) sendFileSystem(packed []byte, hash string) (string, error) {
	header := network.NewHeader(network.ActionFileSystem)
	header.Fields["hash"] = hash
	header.Fields["chunks"] = fmt.Sprint(int64(math.Ceil(float64(len(packed)) / float64(virtualfile.FileChunkSize))))
	// Write hashed password
	var encryptPassword []byte
	if fs.config.Password != "" && fs.config.Password != "skip" {
		decoded, err := hex.DecodeString(fs.config.Password)
		if err != nil {
			return "", err
		}
		encryptPassword = decoded
		hasher := sha256.New()
		hasher.Write(encryptPassword)
		header.Fields["pw-hash"] = hex.EncodeToString(hasher.Sum(nil))
	}
	reader := bytes.NewReader(packed)
	buf := make([]byte, virtualfile.FileChunkSize)
	index := 0
	var id string
	for {
		read, err := virtualfile.ReadFill(reader, buf)
		if err != nil && err != io.EOF {
			return "", err
		}
		header.Fields["index"] = fmt.Sprint(index)
		// Encrypt if needed
		var data []byte
		if encryptPassword != nil {
			data, err = virtualfile.SymmetricEncrypt(encryptPassword, buf[:read])
			if err != nil {
				return "", err
			}
		} else {
			data = buf[:read]
		}
		msgID, err := fs.network.SendAttachment(data, header.String())
		if err != nil {
			return "", err
		}
		if id == "" {
			id = msgID
		}
		header.Action = network.ActionFileSystemChunk
		index++
		if read != len(buf) {
			break
		}
	}
	internal.Logger.Debug("File system has been synchronized")
	return id, nil
}

func (fs *discordFileSystem) syncIfNeeded() error {
	fsMessage, err := fs.network.FetchLastMessage()
	if err != nil {
		return err
	}
	var header *network.DatboxHeader
	var res *http.Response
	var remoteFsChunk []byte
	var remoteFsData bytes.Buffer
	var chunks int64
	var lastMessageID string
	var decryptPassword []byte
	_, hash, err := fs.packFileSystem(true)
	if err != nil {
		return err
	}
	// Channel has no last message
	if fsMessage == nil {
		internal.Logger.Debug("No filesystem message found. Sync needed")
		goto Sync
	}
	lastMessageID = fsMessage.ID
	// Last message has no header
	header, err = network.ParseHeader(fsMessage.Content)
	if err != nil {
		internal.Logger.Debug("Filesystem message does not have valid header. Sync needed")
		goto Sync
	}
	// Header is not filesystem action
	if header.Action != network.ActionFileSystem {
		fs.lastFsMsgID = header.Fields["fs-msg"]
		// Header does not contain filesystem message ID
		if fs.lastFsMsgID == "" {
			internal.Logger.Debug("Last message does not contain filesystem. Sync needed")
			goto Sync
		}
		fsMessage, err = fs.network.FetchMessage(fs.lastFsMsgID)
		// Cannot fetch filesystem message
		if err != nil {
			internal.Logger.Debug("Error fetching filesystem message. Sync needed")
			goto Sync
		}
		header, err = network.ParseHeader(fsMessage.Content)
		// Message has no header
		if err != nil {
			internal.Logger.Debug("Filesystem message does not have valid header. Sync needed")
			goto Sync
		}
		// Header is STILL not filesystem action
		if header.Action != network.ActionFileSystem {
			internal.Logger.Debug("Filesystem message is not of filesystem action. Sync needed")
			goto Sync
		}
	}
	// Check if filesystem is encrypted
	if header.Fields["pw-hash"] != "" {
		hasher := sha256.New()
		var password string
		// Check if stored password is correct
		if fs.config.Password != "" && fs.config.Password != "skip" {
			decoded, err := hex.DecodeString(fs.config.Password)
			if err != nil {
				return err
			}
			// Hash once
			hasher.Write(decoded)
			if header.Fields["pw-hash"] == hex.EncodeToString(hasher.Sum(nil)) {
				decryptPassword = decoded
				goto SkipPassword
			}
		}
		// Ask user for password
		internal.Logger.Debug("Filesystem is encrypted with password different from stored password.")
		internal.Logger.Print("Please enter the password: ")
		{
			pw, err := term.ReadPassword(syscall.Stdin)
			if err != nil {
				return err
			}
			internal.Logger.Println()
			password = string(pw)
		}
		{
			// Hash twice
			hasher.Write([]byte(password))
			hash1 := hasher.Sum(nil)
			hasher = sha256.New()
			hasher.Write(hash1)
			if header.Fields["pw-hash"] != hex.EncodeToString(hasher.Sum(nil)) {
				return errors.New("Passwords don't match")
			}
			decryptPassword = hash1
		}
	SkipPassword:
	}
	fs.lastFsMsgID = fsMessage.ID
	// Check if filesystems are already the same
	if header.Fields["hash"] == hash {
		fs.lastFsHash = hash
		internal.Logger.Debug("Local filesystem matches remote filesystem. Sync not needed")
		return nil
	}
	// Collect all chunks
	if header.Fields["chunks"] == "" {
		internal.Logger.Debug("Filesystem message header does not provide amount of chunks. Assuming to be 1...")
		chunks = 0
	} else {
		chunks, err = strconv.ParseInt(header.Fields["chunks"], 10, 64)
		if err != nil {
			return err
		}
		chunks--
	}
	fs.lastFsHash = header.Fields["hash"]
	res, err = http.Get(fsMessage.Attachments[0].URL)
	if err != nil {
		return err
	}
	// Decrypt if needed
	if decryptPassword != nil {
		all, err := io.ReadAll(res.Body)
		if err != nil {
			return err
		}
		remoteFsChunk, err = virtualfile.SymmetricDecrypt(decryptPassword, all)
		if err != nil {
			return err
		}
	} else {
		remoteFsChunk, err = io.ReadAll(res.Body)
		if err != nil {
			return err
		}
	}
	remoteFsData.Write(remoteFsChunk)
	{
		seenIDs := map[string]bool{}
		seenIDs[lastMessageID] = true
		for chunks > 0 {
			messages, err := fs.network.FetchMessagesSince(fsMessage.ID, int(chunks))
			if err != nil {
				return err
			}
			validMessages := len(messages)
			for _, message := range messages {
				if seenIDs[message.ID] {
					validMessages--
					continue
				}
				seenIDs[message.ID] = true
				header, err := network.ParseHeader(message.Content)
				if err != nil {
					continue
				}
				if header.Action == network.ActionFileSystemChunk && header.Fields["hash"] == fs.lastFsHash {
					res, err = http.Get(message.Attachments[0].URL)
					if err != nil {
						return err
					}
					if decryptPassword != nil {
						all, err := io.ReadAll(res.Body)
						if err != nil {
							return err
						}
						remoteFsChunk, err = virtualfile.SymmetricDecrypt(decryptPassword, all)
						if err != nil {
							return err
						}
					} else {
						remoteFsChunk, err = io.ReadAll(res.Body)
						if err != nil {
							return err
						}
					}
					remoteFsData.Write(remoteFsChunk)
					chunks--
					lastMessageID = message.ID
				}
			}
			if validMessages == 0 {
				return errors.New("No more messages found, but chunks are still incomplete")
			}
		}
	}
	err = fs.mergeFileSystem(remoteFsData.Bytes())
	if err != nil {
		return err
	}
	// Read all logs afterwards
	{
		newFiles := map[string]virtualfile.VirtualFile{}
		lastMessageID = fsMessage.ID
		seenIDs := map[string]bool{}
		seenIDs[lastMessageID] = true
		for {
			messages, err := fs.network.FetchMessagesSince(lastMessageID, 100)
			if err != nil {
				return err
			}
			validMessages := len(messages)
			for _, message := range messages {
				if seenIDs[message.ID] {
					validMessages--
					continue
				}
				seenIDs[message.ID] = true
				header, err := network.ParseHeader(message.Content)
				if err != nil || header.Action == network.ActionFileSystem || header.Action == network.ActionFileSystemChunk || header.Fields["fs-hash"] != fs.lastFsHash {
					continue
				}
				switch header.Action {
				/*case network.ActionBegin:
					if fs.exists(header.Fields["path"]) {
						log.Printf("%s already exists locally. Overwrite with remote version? [y/n]", header.Fields["path"])
						var ans string
						fmt.Scanf("%s", &ans)
						if strings.ToLower(ans) != "y" {
							continue
						}
						fs.Remove(header.Fields["path"])
					}
					version, err := strconv.ParseInt(header.Fields["version"], 10, 8)
					if err != nil {
						version = 1
					}
					file, err := virtualfile.CreateVirtualFile(fs.root, header.Fields["path"], fs.lastFsMsgID, fs.lastFsHash, decryptPassword, fs.network, byte(version))
					if err != nil {
						return err
					}
					err = file.CopyHeader(header)
					if err != nil {
						return err
					}
					err = file.WriteHeader()
					if err != nil {
						return err
					}
				case network.ActionComplete:
					file := newFiles[header.Fields["path"]]
					if file == nil {
						break
					}
					file.WriteMsgID(0)
					checksum, err := hex.DecodeString(header.Fields["hash"])
					if err != nil {
						return err
					}
					err = file.Write(checksum)
					if err != nil {
						return err
					}
					file.Close()
					delete(newFiles, header.Fields["path"])
				case network.ActionFileChunk:
					file := newFiles[header.Fields["path"]]
					if file == nil {
						break
					}
					parsed, err := strconv.ParseUint(message.ID, 10, 64)
					if err != nil {
						return err
					}
					file.WriteMsgID(parsed)*/
				case network.ActionRemove:
					fs.Remove(header.Fields["path"], true)
				}
			}
			if validMessages == 0 {
				break
			}
		}
		for incompletePath, incompleteFile := range newFiles {
			incompleteFile.Close()
			fs.Remove(incompletePath)
		}
	}
	internal.Logger.Debug("Processed all logs")
Sync:
	packed, hash, err := fs.packFileSystem(false)
	if err != nil {
		return err
	}
	if hash == fs.lastFsHash {
		return nil
	}
	id, err := fs.sendFileSystem(packed, hash)
	if err != nil {
		return err
	}
	fs.lastFsMsgID = id
	fs.lastFsHash = hash
	return nil
}

func (fs *discordFileSystem) TranslatePath(virtualPath string) string {
	return path.Join(fs.root, virtualPath)
}

func (fs *discordFileSystem) Mkdir(virtualPath string, perm os.FileMode, all ...bool) error {
	virtualPath = fs.sanitize(virtualPath)
	if len(all) == 1 && all[0] {
		return os.MkdirAll(fs.TranslatePath(virtualPath), perm)
	} else {
		return os.Mkdir(fs.TranslatePath(virtualPath), perm)
	}
}

func (fs *discordFileSystem) Stat(virtualPath string, local ...bool) (os.FileInfo, error) {
	virtualPath = fs.sanitize(virtualPath)
	stat, err := os.Stat(fs.TranslatePath(virtualPath))
	if err != nil {
		return nil, err
	}
	if !stat.IsDir() && (len(local) == 0 || !local[0]) {
		file, err := os.Open(fs.TranslatePath(virtualPath))
		if err != nil {
			return nil, err
		}
		defer file.Close()
		var size int64
		if stat.Size()%8 == 0 {
			// Version 0
			data := make([]byte, 8)
			_, err := io.ReadFull(file, data)
			if err != nil {
				return nil, err
			}
			size = big.NewInt(0).SetBytes(data).Int64()
		} else {
			// Version 1+
			data := make([]byte, 5)
			_, err := io.ReadFull(file, data)
			if err != nil {
				return nil, err
			}
			if string(data[1:]) != "DtBx" {
				return nil, errors.New("Wrong file signature")
			}
			file.Seek(37, io.SeekStart)
			data = make([]byte, 8)
			_, err = io.ReadFull(file, data)
			if err != nil {
				return nil, err
			}
			size = big.NewInt(0).SetBytes(data).Int64()
		}
		newStat := FileInfo{stat: stat, size: size}
		return newStat, nil
	}
	return stat, nil
}

func (fs *discordFileSystem) ReadDir(virtualPath string, long ...bool) ([]os.DirEntry, error) {
	virtualPath = fs.sanitize(virtualPath)
	if stat, err := os.Stat(fs.TranslatePath(virtualPath)); err != nil || !stat.IsDir() {
		if err != nil {
			return nil, err
		}
		return nil, errors.New("Not a directory")
	}
	results := []os.DirEntry{}
	entries, err := os.ReadDir(fs.TranslatePath(virtualPath))
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		stat, err := fs.Stat(path.Join(virtualPath, entry.Name()), len(long) == 0 || !long[0])
		if err != nil {
			return nil, err
		}
		results = append(results, &WrappedDirEntry{entry: entry, info: stat})
	}
	return results, nil
}

func (fs *discordFileSystem) Move(src, dest string) error {
	src = fs.sanitize(src)
	dest = fs.sanitize(dest)
	if !fs.exists(src) {
		return errors.New("Source file doesn't exist")
	}
	if !fs.exists(dest) {
		return errors.New("Destination file doesn't exist")
	}

	return os.Rename(fs.TranslatePath(src), fs.TranslatePath(dest))
}

func (fs *discordFileSystem) recursiveIncrementReference(eitherPath string) error {
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

func (fs *discordFileSystem) Copy(src, dest string) error {
	src = fs.sanitize(src)
	dest = fs.sanitize(dest)
	err := cp.Copy(fs.TranslatePath(src), fs.TranslatePath(dest))
	if err != nil {
		return err
	}
	err = fs.recursiveIncrementReference(fs.TranslatePath(src))
	if err != nil {
		return err
	}
	return fs.saveReference()
}

func (fs *discordFileSystem) Remove(virtualPath string, options ...bool) error {
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
	header := network.NewHeader(network.ActionRemove)
	header.Fields["fs-msg"] = fs.lastFsMsgID
	header.Fields["fs-hash"] = fs.lastFsHash
	header.Fields["path"] = virtualPath
	if stat.IsDir() {
		if !recursive {
			return errors.New("Cannot remove directory. Consider setting recursive to true")
		}
		if fs.initialized {
			err = fs.network.SendMessage(header)
			if err != nil {
				return err
			}
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
		if fs.initialized {
			err = fs.network.SendMessage(header)
			if err != nil {
				return err
			}
		}
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
				internal.Logger.Error("Cannot delete remote. Another file referencing the same chunks exist")
			} else {
				file, err := virtualfile.OpenVirtualFile(fs.root, virtualPath, fs.lastFsMsgID, fs.lastFsHash, fs.network)
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

type TransferResult struct {
	StartTime time.Time
	EndTime   time.Time
	Size      uint64
	Chunks    int
	Checksum  []byte
}

func (fs *discordFileSystem) Upload(fileReader io.ReadCloser, virtualPath string, size int64, fileVersion byte) (result TransferResult, err error) {
	virtualPath = fs.sanitize(virtualPath)

	stat, err := fs.Stat(virtualPath, true)
	if err == nil && stat.IsDir() {
		virtualPath = path.Join(virtualPath)
	}

	virtualDir := path.Join(fs.root, path.Dir(virtualPath))
	os.MkdirAll(virtualDir, 0755)
	if _, err = os.Stat(virtualDir); err != nil {
		err = errors.New("Failed to create directory")
		return
	}
	if _, err = os.Stat(path.Join(fs.root, virtualPath)); err == nil {
		err = errors.New("File already exists in virtual file system")
		return
	}

	result.StartTime = time.Now()
	var globalPassword []byte
	if fs.config.Password != "" && fs.config.Password != "skip" {
		globalPassword, err = hex.DecodeString(fs.config.Password)
		if err != nil {
			return
		}
	}
	file, err := virtualfile.CreateVirtualFile(fs.root, virtualPath, fs.lastFsMsgID, fs.lastFsHash, globalPassword, fs.network, fileVersion)
	if err != nil {
		return
	}
	err = file.OpenOrCreate()
	if err != nil {
		return
	}
	if !file.WriteMode() {
		err = errors.New("File should be in write mode")
		return
	}

	// Defer syncing
	defer func() {
		if err != nil {
			return
		}
		packed, hash, err := fs.packFileSystem(false)
		if err != nil {
			internal.Logger.Error(err)
			return
		}
		id, err := fs.sendFileSystem(packed, hash)
		if err != nil {
			internal.Logger.Error(err)
			return
		}
		fs.lastFsMsgID = id
		fs.lastFsHash = hash
	}()

	eventSignal := make(chan virtualfile.TransferEvent)
	go file.Upload(bufio.NewReaderSize(fileReader, virtualfile.FileChunkSize), size, eventSignal)
	for {
		event := <-eventSignal
		if event.Done {
			if event.Err != nil {
				fs.Remove(virtualPath)
				err = event.Err
				return
			}
			internal.Logger.Printf("\rProgress: 100%% (%s / %s, %s/s, %d / %d chunks)\n", humanize.Bytes(uint64(file.Size())), humanize.Bytes(uint64(file.Size())), humanize.Bytes(uint64(file.Size()/int64(time.Since(result.StartTime).Seconds()))), event.CurrentChunks, event.TotalChunks)
			break
		} else {
			internal.Logger.Printf("\rProgress: %03d%% (%s / %s, %s/s, %d / %d chunks)\r", int(100*event.CurrentBytes/event.TotalBytes), humanize.Bytes(uint64(event.CurrentBytes)), humanize.Bytes(uint64(event.TotalBytes)), humanize.Bytes(uint64(event.CurrentBytes/int64(time.Since(result.StartTime).Seconds()))), event.CurrentChunks, event.TotalChunks)
		}
	}

	result.EndTime = time.Now()
	result.Size = uint64(file.Size())
	result.Chunks = file.Chunks()
	result.Checksum = file.Checksum()

	return
}

func (fs *discordFileSystem) Download(fileWriter io.WriteCloser, virtualPath string) (result TransferResult, err error) {
	virtualPath = fs.sanitize(virtualPath)
	_, err = fs.Stat(virtualPath, true)
	if err != nil {
		err = errors.New("Virtual path " + path.Join(fs.root, virtualPath) + " doesn't exist")
		return
	}

	result.StartTime = time.Now()
	file, err := virtualfile.OpenVirtualFile(fs.root, virtualPath, fs.lastFsMsgID, fs.lastFsHash, fs.network)
	if err != nil {
		return
	}
	defer file.Close()
	err = file.OpenOrCreate()
	if err != nil {
		return
	}
	if file.WriteMode() {
		err = errors.New("File should not be in write mode")
		return
	}
	eventSignal := make(chan virtualfile.TransferEvent)
	go file.Download(fileWriter, eventSignal)
	for {
		event := <-eventSignal
		elapsed := max(time.Since(result.StartTime).Seconds(), 1)
		if event.Done {
			if event.Err != nil {
				err = event.Err
				return
			}
			internal.Logger.Printf("Progress: 100%% (%s / %s, %s/s, %d / %d chunks)\n", humanize.Bytes(uint64(file.Size())), humanize.Bytes(uint64(file.Size())), humanize.Bytes(uint64(file.Size()/int64(elapsed))), event.CurrentChunks, event.TotalChunks)
			break
		} else {
			internal.Logger.Printf("Progress: %03d%% (%s / %s, %s/s, %d / %d chunks)\r", int(100*event.CurrentBytes/event.TotalBytes), humanize.Bytes(uint64(event.CurrentBytes)), humanize.Bytes(uint64(event.TotalBytes)), humanize.Bytes(uint64(event.CurrentBytes/int64(elapsed))), event.CurrentChunks, event.TotalChunks)
		}
	}

	return
}
