package capability

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	JavaRegistryPath  = "shell/app/src/main/java/com/berba/shell/CapabilityRegistry.java"
	DocsPath          = "docs/capabilities.md"
	ShellManifestPath = "shell/shell.json"
)

// Generate 把三类生成物写入项目根目录。
func Generate(r *Registry, root string) error {
	java, err := GenerateJavaRegistry(r)
	if err != nil {
		return err
	}
	docs, err := RenderDocs(r)
	if err != nil {
		return err
	}
	shell, err := GenerateShellManifest(r)
	if err != nil {
		return err
	}
	for _, item := range []struct {
		rel  string
		data []byte
	}{
		{JavaRegistryPath, java},
		{DocsPath, docs},
		{ShellManifestPath, shell},
	} {
		full := filepath.Join(root, filepath.FromSlash(item.rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, item.data, 0o644); err != nil {
			return fmt.Errorf("写入 %s 失败: %w", item.rel, err)
		}
	}
	return nil
}
