package voicetype

import (
	"bytes"
	"os"
	"testing"
)

func TestUpdateEmbedsInstaller(t *testing.T) {
	if !bytes.Contains(installerScript, []byte("--version")) || !bytes.Contains(installerScript, []byte("sha256sum -c")) {
		t.Fatal("embedded installer is not the release installer")
	}
	path, err := writeInstaller()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != int64(len(installerScript)) || info.Mode().Perm() != 0o700 {
		t.Fatalf("installer file: size=%d mode=%o", info.Size(), info.Mode().Perm())
	}
	written, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(written, installerScript) {
		t.Fatalf("installer content mismatch: %v", err)
	}
}
