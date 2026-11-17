//go:build linux || darwin

package voicetype

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func ensurePrivateDirectory(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("private directory path must be absolute")
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open private directory: %w", err)
	}
	defer unix.Close(fd)
	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		return err
	}
	if info.Uid != uint32(os.Getuid()) {
		return errors.New("private directory must be owned by you")
	}
	if info.Mode&0200 == 0 {
		return errors.New("private directory is read-only")
	}
	return unix.Fchmod(fd, 0700)
}

func readPrivateFile(path string) ([]byte, error) {
	file, err := openPrivateFile(path, unix.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}

func openPrivateFile(path string, flags int) (*os.File, error) {
	fd, err := unix.Open(path, flags|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		file.Close()
		return nil, err
	}
	if info.Mode&unix.S_IFMT != unix.S_IFREG || info.Uid != uint32(os.Getuid()) || info.Nlink != 1 {
		file.Close()
		return nil, errors.New("private file must be a regular file owned by you")
	}
	return file, nil
}

// Publish once with a hard link so even concurrent migrations cannot replace an
// existing destination. Keep the legacy file intact, including on any failure.
func copyLegacyConfig(source, destination string) error {
	if _, err := os.Lstat(destination); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	fd, err := unix.Open(source, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read legacy configuration: %w", err)
	}
	file := os.NewFile(uintptr(fd), source)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if !info.Mode().IsRegular() || stat.Uid != uint32(os.Getuid()) {
		return errors.New("legacy configuration must be a regular file owned by you")
	}
	const limit = 4 << 20
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return err
	}
	if len(data) > limit {
		return errors.New("legacy configuration is larger than 4 MiB; migrate it manually")
	}
	if err := ensurePrivateDirectory(filepath.Dir(destination)); err != nil {
		return err
	}
	stage, err := os.CreateTemp(filepath.Dir(destination), ".legacy-config-*")
	if err != nil {
		return err
	}
	defer os.Remove(stage.Name())
	_, writeErr := stage.Write(data)
	if writeErr == nil {
		writeErr = stage.Sync()
	}
	closeErr := stage.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	if err := os.Link(stage.Name(), destination); err != nil && !os.IsExist(err) {
		return fmt.Errorf("publish native configuration: %w", err)
	}
	return nil
}
