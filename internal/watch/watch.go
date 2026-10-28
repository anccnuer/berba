// Package watch 通过轮询文件元信息检测项目变更。
package watch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"time"
)

// DefaultSkip 是构建产物与常见无关目录。
var DefaultSkip = []string{".git", ".berba", "dist", "node_modules", ".idea", ".vscode"}

// Snapshot 返回目录树的指纹（路径、大小、修改时间）。
func Snapshot(root string, skip []string) (string, error) {
	skipSet := make(map[string]bool, len(skip))
	for _, name := range skip {
		skipSet[name] = true
	}
	var lines []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && skipSet[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			// 编辑器原子保存期间文件可能瞬时消失，跳过即可。
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		lines = append(lines, fmt.Sprintf("%s|%d|%d", filepath.ToSlash(rel), info.Size(), info.ModTime().UnixNano()))
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(lines)
	h := sha256.New()
	for _, line := range lines {
		h.Write([]byte(line))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Watch 在检测到变更后等待一个轮询周期确认稳定，再调用 onChange。
func Watch(ctx context.Context, root string, interval time.Duration, skip []string, onChange func()) error {
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	last, err := Snapshot(root, skip)
	if err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	pending := false
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			cur, err := Snapshot(root, skip)
			if err != nil {
				// 目录被删除或暂时不可读时保留上次快照，继续等待。
				continue
			}
			if cur != last {
				last = cur
				pending = true
				continue
			}
			if pending {
				pending = false
				onChange()
			}
		}
	}
}
