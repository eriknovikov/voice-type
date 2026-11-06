package voicetype

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Never unlink this file: replacing its inode would let two engines hold locks
// on different files. Closing the descriptor (including process death) releases it.
func acquireEngineLock() (*os.File, error) {
	dir := engineRuntimeDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create Voice Type runtime directory: %w", err)
	}
	dirFD, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open Voice Type runtime directory: %w", err)
	}
	defer unix.Close(dirFD)
	var info unix.Stat_t
	if err := unix.Fstat(dirFD, &info); err != nil {
		return nil, err
	}
	if info.Uid != uint32(os.Getuid()) || info.Mode&0077 != 0 {
		return nil, errors.New("Voice Type runtime directory must be owned by you and private (0700)")
	}
	fd, err := unix.Openat(dirFD, "engine.lock", unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, fmt.Errorf("open Voice Type engine lock: %w", err)
	}
	file := os.NewFile(uintptr(fd), filepath.Join(dir, "engine.lock"))
	if err := unix.Fstat(fd, &info); err != nil {
		file.Close()
		return nil, err
	}
	if info.Mode&unix.S_IFMT != unix.S_IFREG || info.Uid != uint32(os.Getuid()) || info.Mode&0077 != 0 || info.Nlink != 1 {
		file.Close()
		return nil, errors.New("Voice Type engine lock must be a private regular file owned by you (0600)")
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, errors.New("another Voice Type engine is running; quit the other Voice Type instance first")
		}
		return nil, fmt.Errorf("lock Voice Type engine: %w", err)
	}
	return file, nil
}

func engineRuntimeDir() string {
	if base := os.Getenv("XDG_RUNTIME_DIR"); base != "" {
		return filepath.Join(base, "voice-type")
	}
	return filepath.Dir(workspaceFilePath())
}
