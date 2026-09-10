package datboxdiscord

import (
	"io/fs"
)

type WrappedDirEntry struct {
	entry fs.DirEntry
	info  fs.FileInfo
}

func (e *WrappedDirEntry) Name() string {
	return e.entry.Name()
}

func (e *WrappedDirEntry) IsDir() bool {
	return e.entry.IsDir()
}

func (e *WrappedDirEntry) Type() fs.FileMode {
	return e.entry.Type()
}

func (e *WrappedDirEntry) Info() (fs.FileInfo, error) {
	return e.info, nil
}
