// Package publish 提供原子替换产物的公共辅助。
package publish

import "os"

// RenameOver 把 src 重命名为 dst；dst 已存在时先删除再重试（Windows 不允许 rename 覆盖已存在文件）。
// 语义与 build.Publish、release.writeZip 中原有的内联实现逐字一致。
func RenameOver(src, dst string) error {
	if err := os.Rename(src, dst); err != nil {
		if removeErr := os.Remove(dst); removeErr != nil && !os.IsNotExist(removeErr) {
			return err
		}
		if err := os.Rename(src, dst); err != nil {
			return err
		}
	}
	return nil
}
