package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"berba/internal/capability"
)

func main() {
	root := flag.String("root", "", "项目根目录（默认自动查找 go.mod）")
	flag.Parse()

	dir := *root
	if dir == "" {
		var err error
		dir, err = findRoot()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	registry, err := capability.LoadBuiltin()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := capability.Generate(registry, dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func findRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("未找到 go.mod，请用 -root 指定项目根目录")
		}
		dir = parent
	}
}
