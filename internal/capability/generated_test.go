package capability

import (
	"os"
	"path/filepath"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join("..", "..")
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("cannot locate repo root: %v", err)
	}
	return root
}

func TestGeneratedFilesUpToDate(t *testing.T) {
	reg, err := LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}
	root := repoRoot(t)
	java, err := GenerateJavaRegistry(reg)
	if err != nil {
		t.Fatal(err)
	}
	docs, err := RenderDocs(reg)
	if err != nil {
		t.Fatal(err)
	}
	shell, err := GenerateShellManifest(reg)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		rel  string
		data []byte
	}{
		{JavaRegistryPath, java},
		{DocsPath, docs},
		{ShellManifestPath, shell},
	} {
		got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(item.rel)))
		if err != nil {
			t.Fatalf("%s: %v（请运行 go generate ./internal/capability）", item.rel, err)
		}
		if string(got) != string(item.data) {
			t.Fatalf("%s 已过期，请运行 go generate ./internal/capability", item.rel)
		}
	}
}

func TestGenerateWritesAllFiles(t *testing.T) {
	reg, err := LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := Generate(reg, root); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{JavaRegistryPath, DocsPath, ShellManifestPath} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
	}
}
