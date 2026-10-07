package ownedpath

import (
	"errors"
	"os"
)

var ErrOwnershipChanged = errors.New("owned_directory_identity_changed")

type Directory struct {
	Path string
	info os.FileInfo
}

func TempDir(parent, pattern string) (Directory, error) {
	path, err := os.MkdirTemp(parent, pattern)
	if err != nil {
		return Directory{}, err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Directory{}, ErrOwnershipChanged
	}
	return Directory{path, info}, nil
}

func (d Directory) Check() error {
	current, err := os.Lstat(d.Path)
	if err != nil || d.info == nil || !current.IsDir() || !os.SameFile(d.info, current) {
		return ErrOwnershipChanged
	}
	return nil
}

func (d Directory) Close() error {
	if _, err := os.Lstat(d.Path); os.IsNotExist(err) {
		return nil
	}
	if err := d.Check(); err != nil {
		return err
	}
	return os.RemoveAll(d.Path)
}
