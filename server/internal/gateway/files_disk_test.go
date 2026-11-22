package gateway

import (
	"errors"
	"io"
	"os"
	"testing"

	"github.com/easyremote/server/internal/errcode"
)

// ER-903f：写入失败必须可观察、返回磁盘错误，且不留下半写文件。
func TestSavePartDiskFailureLeavesNoPartialFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700)
	a := &App{}
	_, err := a.savePart(dir, readerOf("hello"), 1024)
	if !errcode.Is(err, errcode.FileDiskFull) {
		t.Fatalf("want FILE_DISK_FULL, got %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("partial files left: %v", entries)
	}
}

func TestSavePartStreamErrorRemovesPartialFile(t *testing.T) {
	dir := t.TempDir()
	a := &App{}
	r := &failingReader{data: []byte("partial"), err: errors.New("disk full")}
	_, err := a.savePart(dir, r, 1024)
	if err == nil {
		t.Fatal("want error")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("partial files left: %v", entries)
	}
}

func readerOf(s string) io.Reader { return &stringReader{s: s} }

type stringReader struct {
	s string
	i int
}

func (r *stringReader) Read(p []byte) (int, error) {
	if r.i >= len(r.s) {
		return 0, io.EOF
	}
	n := copy(p, r.s[r.i:])
	r.i += n
	return n, nil
}

type failingReader struct {
	data []byte
	done bool
	err  error
}

func (r *failingReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, r.err
	}
	r.done = true
	return copy(p, r.data), nil
}
