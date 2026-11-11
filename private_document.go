package voicetype

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// BackupDirectory is separate from config/state so removal of either primary
// directory does not also remove its last valid copy.
func BackupDirectory() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.New("cannot locate your home directory")
		}
		base = filepath.Join(home, ".local", "share")
	}
	app := filepath.Join(base, "voice-type")
	path := filepath.Join(app, "backups")
	if err := os.MkdirAll(path, 0700); err != nil {
		return "", fmt.Errorf("create private backup directory: %w", err)
	}
	for _, dir := range []string{app, path} {
		if err := os.Chmod(dir, 0700); err != nil {
			return "", fmt.Errorf("protect backup directory: %w", err)
		}
	}
	return path, nil
}

// AtomicPrivateWrite publishes only complete, synced files. It does not change
// permissions on shared parent directories such as the user's home.
func AtomicPrivateWrite(path string, data []byte) error { return atomicPrivateWrite(path, data) }

func atomicPrivateWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".voice-type-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func backupPath(name string) (string, error) {
	if name == "" || filepath.Base(name) != name {
		return "", errors.New("invalid backup name")
	}
	dir, err := BackupDirectory()
	return filepath.Join(dir, name), err
}

func preservePrivateDocument(name string, data []byte) error {
	path, err := backupPath(name)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	path += ".corrupt-" + hex.EncodeToString(sum[:8])
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		return nil
	}
	return atomicPrivateWrite(path, data)
}

// ReadPrivateDocument returns nil data when there is no valid primary or
// backup. Callers then supply safe defaults. Unreadable files are never reset.
func ReadPrivateDocument(path, backupName string, validate func([]byte) error) ([]byte, string, error) {
	data, err := os.ReadFile(path)
	missing := os.IsNotExist(err)
	if err != nil && !missing {
		return nil, "", fmt.Errorf("read settings; check file permissions: %w", err)
	}
	if err == nil && validate(data) == nil {
		if err := os.Chmod(path, 0600); err != nil {
			return nil, "", fmt.Errorf("protect settings file: %w", err)
		}
		backup, err := backupPath(backupName)
		if err != nil {
			return nil, "", err
		}
		old, readErr := os.ReadFile(backup)
		if readErr != nil && !os.IsNotExist(readErr) {
			return nil, "", fmt.Errorf("read settings backup: %w", readErr)
		}
		if !bytes.Equal(old, data) {
			if err := atomicPrivateWrite(backup, data); err != nil {
				return nil, "", fmt.Errorf("save settings backup: %w", err)
			}
		}
		return data, "", nil
	}
	if !missing {
		if err := preservePrivateDocument(backupName, data); err != nil {
			return nil, "", fmt.Errorf("preserve damaged settings: %w", err)
		}
	}
	backup, err := backupPath(backupName)
	if err != nil {
		return nil, "", err
	}
	last, err := os.ReadFile(backup)
	if err != nil && !os.IsNotExist(err) {
		return nil, "", fmt.Errorf("read settings backup: %w", err)
	}
	if err == nil && validate(last) == nil {
		if err := atomicPrivateWrite(path, last); err != nil {
			return nil, "", fmt.Errorf("restore settings backup: %w", err)
		}
		return last, "Settings restored.", nil
	}
	if err == nil {
		if err := preservePrivateDocument(backupName+".backup", last); err != nil {
			return nil, "", fmt.Errorf("preserve damaged settings backup: %w", err)
		}
		return nil, "Settings could not be restored. Defaults were loaded.", nil
	}
	if missing {
		return nil, "", nil
	}
	return nil, "Damaged settings were preserved. Defaults were loaded.", nil
}

// WritePrivateDocument is used only after validation. The backup is synced
// before replacing the primary, keeping at least one complete copy on disk.
func WritePrivateDocument(path, backupName string, data []byte) error {
	backup, err := backupPath(backupName)
	if err != nil {
		return err
	}
	if err := atomicPrivateWrite(backup, data); err != nil {
		return fmt.Errorf("save settings backup: %w", err)
	}
	if err := atomicPrivateWrite(path, data); err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	return nil
}
