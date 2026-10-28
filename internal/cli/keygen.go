package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"guiapp/internal/sign"
)

func runKeygen(args []string) error {
	fsFlags := newFlagSet("keygen", "生成 APK 签名密钥")
	outPath := fsFlags.String("out", "", "密钥输出路径（默认 ./.guiapp/keys/release.pem）")
	force := fsFlags.Bool("force", false, "覆盖已有密钥（会破坏升级链，谨慎使用）")
	if err := parseArgs(fsFlags, args); err != nil {
		return err
	}
	path := *outPath
	if path == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		path = filepath.Join(cwd, ".guiapp", "keys", "release.pem")
	}
	if _, err := os.Stat(path); err == nil && !*force {
		key, err := sign.Load(path)
		if err != nil {
			return fmt.Errorf("密钥已存在但无法读取: %w（使用 --force 覆盖）", err)
		}
		fmt.Fprintf(out, "密钥已存在: %s\n指纹 SHA-256: %s\n", path, key.Fingerprint())
		return nil
	}
	key, err := sign.Generate(path)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "已生成密钥: %s\n指纹 SHA-256: %s\n\n请离线备份该文件：它是应用身份，丢失后无法覆盖升级。\n", path, key.Fingerprint())
	return nil
}
