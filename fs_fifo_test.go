//go:build !windows

package polipage_test

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
)

func TestRenderToFile_fifoTargetIsIOErrorWithoutRendering(t *testing.T) {
	t.Parallel()
	var renders atomic.Int32
	server := twoHopServer(t, []byte("%PDF-1.4\nx"), func([]byte) { renders.Add(1) })
	client := newTestClient(t, server)

	dir := t.TempDir()
	dst := filepath.Join(dir, "out.pdf")
	if err := syscall.Mkfifo(dst, 0o600); err != nil {
		t.Skipf("cannot create a FIFO here: %v", err)
	}

	// Judged by its attributes: opening the FIFO would block without a reader.
	err := renderToFile(client, dst)
	pErr := wantIOErrorNaming(t, err, dst)
	if want := "Failed to write PDF: " + dst + ": not a regular file"; pErr.Message != want {
		t.Errorf("Message = %q, want %q", pErr.Message, want)
	}
	if n := renders.Load(); n != 0 {
		t.Errorf("render calls = %d, want 0", n)
	}
	info, statErr := os.Lstat(dst)
	if statErr != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Errorf("the FIFO was replaced: %v, %v", info, statErr)
	}
	wantOnlyEntries(t, dir, "out.pdf")
}
