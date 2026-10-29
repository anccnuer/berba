// Package fswalk 提供拒绝符号链接的目录遍历辅助。
package fswalk

import (
	"fmt"
	"os"
	"path/filepath"
)

// Files 自 root 递归遍历普通文件，目录跳过，遇到符号链接返回错误。
// relSlash 为相对 root 的正斜杠路径，absPath 为文件的完整路径。
func Files(root string, fn func(relSlash, absPath string) error) error {
	return filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("不允许符号链接: %s", p)
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		return fn(filepath.ToSlash(rel), p)
	})
}
