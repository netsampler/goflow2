package file

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

// TestSendDuringReopen rotates the output the way logrotate does (rename,
// then SIGHUP) while several goroutines send. Every message must be written
// whole, exactly once, to one of the files.
func TestSendDuringReopen(t *testing.T) {
	const (
		writers   = 8
		perWriter = 2000
		rotations = 200
	)

	dir := t.TempDir()
	path := filepath.Join(dir, "flows.json")
	d := &FileDriver{
		fileDestination: path,
		lineSeparator:   "\n",
		lock:            &sync.Mutex{},
	}
	if err := d.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	errs := make(chan error, writers*perWriter)
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				if err := d.Send(nil, []byte(fmt.Sprintf(`{"writer":%d,"seq":%d}`, w, i))); err != nil {
					errs <- err
				}
			}
		}(w)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for r := 0; r < rotations; r++ {
			if err := os.Rename(path, fmt.Sprintf("%s.%d", path, r)); err != nil {
				t.Errorf("rename: %v", err)
				return
			}
			d.reloadCh <- syscall.SIGHUP
			// as a rotation tool does, wait for the reopened file before
			// the next rename
			for {
				if _, err := os.Stat(path); err == nil {
					break
				}
				time.Sleep(time.Millisecond)
			}
		}
	}()

	wg.Wait()
	<-done
	if err := d.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	close(errs)
	for err := range errs {
		t.Errorf("send: %v", err)
	}

	files, err := filepath.Glob(path + "*")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	seen := make(map[string]int)
	for _, name := range files {
		f, err := os.Open(name)
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			seen[scanner.Text()]++
		}
		if err := f.Close(); err != nil {
			t.Fatalf("close %s: %v", name, err)
		}
	}
	for w := 0; w < writers; w++ {
		for i := 0; i < perWriter; i++ {
			line := fmt.Sprintf(`{"writer":%d,"seq":%d}`, w, i)
			if n := seen[line]; n != 1 {
				t.Errorf("line %s written %d times", line, n)
			}
			delete(seen, line)
		}
	}
	for line := range seen {
		t.Errorf("unexpected line %q", line)
	}
}

// BenchmarkSend measures Send with parallel writers to one file.
func BenchmarkSend(b *testing.B) {
	d := &FileDriver{
		fileDestination: filepath.Join(b.TempDir(), "flows.json"),
		lineSeparator:   "\n",
		lock:            &sync.Mutex{},
	}
	if err := d.Init(); err != nil {
		b.Fatalf("init: %v", err)
	}
	b.Cleanup(func() {
		if err := d.Close(); err != nil {
			b.Errorf("close: %v", err)
		}
	})
	msg := make([]byte, 600)
	for i := range msg {
		msg[i] = 'x'
	}
	b.SetBytes(int64(len(msg) + 1))
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := d.Send(nil, msg); err != nil {
				b.Error(err)
				return
			}
		}
	})
}
