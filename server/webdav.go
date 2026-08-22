package server

import (
	"context"
	"datbox/server/virtualfile"
	"errors"
	"io/fs"
	"os"

	"golang.org/x/net/webdav"
)

type WebdavFileSystem struct {
	fs *DatboxFileSystem

	webdav.FileSystem
}

func (wdfs *WebdavFileSystem) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	if flag&os.O_APPEND != 0 {
		return nil, errors.New("Please don't use O_APPEND")
	}
	flag = flag & ^os.O_TRUNC
	// If opened as write-only, re-open as read-write
	if flag&os.O_WRONLY != 0 {
		flag = flag & ^os.O_WRONLY | os.O_RDWR
	}
	file, err := os.OpenFile(wdfs.fs.TranslatePath(name), flag, perm)
	if err != nil {
		return nil, err
	}
	var fileVersion int
	buf := make([]byte, 1)
	read, err := file.Read(buf)
	if err != nil || read != 1 {
		fileVersion = 1
	} else {
		fileVersion = int(buf[0])
	}
	vFile, err := virtualfile.NewVirtualFile(file, wdfs.fs.root, name, wdfs.fs.lastFsMsgID, wdfs.fs.lastFsHash, wdfs.fs.globalPassword, wdfs.fs.network, fileVersion)
	if err != nil {
		return nil, err
	}
	return &WebdavFile{
		Path:        name,
		wdfs:        wdfs,
		virtualFile: vFile,
	}, nil
}

func (wdfs *WebdavFileSystem) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	return wdfs.fs.Mkdir(name, perm)
}

func (wdfs *WebdavFileSystem) RemoveAll(ctx context.Context, name string) error {
	return wdfs.fs.Remove(name, true)
}

func (wdfs *WebdavFileSystem) Rename(ctx context.Context, oldName, newName string) error {
	return wdfs.fs.Move(oldName, newName)
}

func (wdfs *WebdavFileSystem) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	return wdfs.fs.Stat(name)
}

type WebdavFile struct {
	Path string

	wdfs        *WebdavFileSystem
	virtualFile virtualfile.VirtualFile
}

func (file *WebdavFile) Close() error {
	if file.virtualFile != nil {
		return file.virtualFile.Close()
	}
	return errors.New("File not opened")
}

func (file *WebdavFile) Read(buf []byte) (int, error) {
	if file.virtualFile != nil {
		return 0, errors.New("File not opened")
	}
	return file.virtualFile.Read(buf)
}

func (file *WebdavFile) Seek(offset int64, whence int) (int64, error) {
	if file.virtualFile != nil {
		return 0, errors.New("File not opened")
	}
	return file.virtualFile.Seek(offset, whence)
}

func (file *WebdavFile) Readdir(count int) ([]fs.FileInfo, error) {
	entries, err := file.wdfs.fs.ReadDir(file.Path, true)
	if err != nil {
		return nil, err
	}
	files := make([]fs.FileInfo, len(entries))
	for _, entry := range entries {
		files = append(files, entry.Stat)
	}
	return files, nil
}

func (file *WebdavFile) Stat() (fs.FileInfo, error) {
	return file.wdfs.Stat(context.Background(), file.Path)
}

func (file *WebdavFile) Write(p []byte) (n int, err error) {
	if file.virtualFile != nil {
		return 0, errors.New("File not opened")
	}
	return file.virtualFile.Write(p)
}
