package apk

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	alignARSC = 4
	alignSO   = 16384
)

// ReadEntries 读取一个 APK/ZIP 的全部文件条目。
func ReadEntries(path string) ([]Entry, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("读取 %s 失败: %w", filepath.Base(path), err)
	}
	defer zr.Close()

	entries := make([]Entry, 0, len(zr.File))
	for _, f := range zr.File {
		name := f.Name
		if strings.HasSuffix(name, "/") {
			continue
		}
		if !safeEntryName(name) {
			return nil, fmt.Errorf("压缩包条目名不合法: %q", name)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		entries = append(entries, classify(Entry{Name: name, Data: data}))
	}
	return entries, nil
}

func safeEntryName(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") {
		return false
	}
	clean := path.Clean(name)
	if clean != name || clean == ".." || strings.HasPrefix(clean, "../") {
		return false
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// classify 按 Android 安装器要求决定压缩与对齐方式。
func classify(e Entry) Entry {
	name := strings.ReplaceAll(e.Name, "\\", "/")
	switch {
	case name == "resources.arsc":
		e.Store, e.Align = true, alignARSC
	case strings.HasPrefix(name, "lib/") && strings.HasSuffix(name, ".so"):
		e.Store, e.Align = true, alignSO
	default:
		e.Store, e.Align = false, 0
	}
	return e
}

// Assemble 合并基础 APK 与额外条目并输出对齐后的未签名 APK。
func Assemble(baseAPK string, extra []Entry) ([]byte, error) {
	base, err := ReadEntries(baseAPK)
	if err != nil {
		return nil, err
	}
	entries := append([]Entry(nil), base...)
	for _, e := range extra {
		if !safeEntryName(e.Name) {
			return nil, fmt.Errorf("资源条目名不合法: %q", e.Name)
		}
		entries = append(entries, classify(e))
	}
	return Build(entries)
}

// DirEntries 递归收集目录下的文件，目标名前缀为 prefix。
func DirEntries(root, prefix string) ([]Entry, error) {
	var entries []Entry
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("不允许符号链接: %s", p)
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		name := path.Join(prefix, filepath.ToSlash(rel))
		entries = append(entries, Entry{Name: name, Data: data})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}
