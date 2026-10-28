package cli

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"berba/internal/config"
	"berba/templates"
)

func runInit(args []string) error {
	fsFlags := newFlagSet("init", "创建 berba 项目脚手架")
	name := fsFlags.String("name", "", "应用名称（默认 示例应用）")
	pkg := fsFlags.String("package", "", "应用包名（默认 com.example.demo）")
	force := fsFlags.Bool("force", false, "覆盖已存在的 app.json")
	if err := parseArgs(fsFlags, args); err != nil {
		return err
	}
	target := fsFlags.Arg(0)
	if target == "" {
		target = "."
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return err
	}

	appJSON := filepath.Join(abs, "app.json")
	if _, err := os.Stat(appJSON); err == nil && !*force {
		return fmt.Errorf("%s 已存在（使用 --force 覆盖）", appJSON)
	}
	if *pkg != "" {
		if err := config.ValidatePackage(*pkg); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return err
	}

	root := templates.Init()
	err = fs.WalkDir(root, ".", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		dest := filepath.Join(abs, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		if p == "app.json" && *force {
			// 继续覆盖
		} else if _, statErr := os.Stat(dest); statErr == nil {
			fmt.Fprintf(out, "  跳过已存在文件 %s\n", relPath(abs, dest))
			return nil
		}
		data, err := fs.ReadFile(root, p)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0o644)
	})
	if err != nil {
		return err
	}

	if *name != "" || *pkg != "" {
		if err := rewriteAppJSON(appJSON, *name, *pkg); err != nil {
			return err
		}
	}

	fmt.Fprintf(out, "已创建项目: %s\n\n下一步:\n  cd %s\n  berba build\n", abs, relPath(".", abs))
	return nil
}

func rewriteAppJSON(path, name, pkg string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if name != "" {
		raw["name"] = name
	}
	if pkg != "" {
		raw["package"] = pkg
	}
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}

func relPath(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return target
	}
	return rel
}
