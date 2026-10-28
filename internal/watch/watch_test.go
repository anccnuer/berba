package watch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSnapshotIgnoresSkippedDirs(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.html", "a")
	first, err := Snapshot(root, DefaultSkip)
	if err != nil {
		t.Fatal(err)
	}
	write("dist/app.apk", "binary")
	second, err := Snapshot(root, DefaultSkip)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("dist 目录变化不应影响快照")
	}
	write("index.html", "abc")
	third, err := Snapshot(root, DefaultSkip)
	if err != nil {
		t.Fatal(err)
	}
	if third == second {
		t.Fatal("源文件变化应改变快照")
	}
}

func TestWatchFiresOnChange(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "index.html")
	if err := os.WriteFile(file, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fired := make(chan struct{}, 4)
	done := make(chan error, 1)
	go func() {
		done <- Watch(ctx, root, 20*time.Millisecond, DefaultSkip, func() {
			select {
			case fired <- struct{}{}:
			default:
			}
		})
	}()

	time.Sleep(100 * time.Millisecond)
	if err := os.WriteFile(file, []byte("changed-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fired:
	case <-time.After(3 * time.Second):
		t.Fatal("变更后未触发回调")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
