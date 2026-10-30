package capability

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strings"
)

//go:embed specs/*.json
var specFS embed.FS

// LoadBuiltin 加载内嵌规格，并校验名称、事件与组件引用。
func LoadBuiltin() (*Registry, error) {
	entries, err := fs.ReadDir(specFS, "specs")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, fmt.Errorf("未找到内嵌能力规格")
	}

	var specs []*Spec
	seenSpec := map[string]bool{}
	seenModule := map[string]string{}
	seenGlobal := map[string]string{}
	seenEvent := map[string]string{}
	for _, name := range names {
		data, err := specFS.ReadFile("specs/" + name)
		if err != nil {
			return nil, err
		}
		spec, err := parseSpec(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if seenSpec[spec.Name] {
			return nil, fmt.Errorf("重复的能力名称: %q", spec.Name)
		}
		seenSpec[spec.Name] = true
		for module := range spec.Modules {
			if prev, ok := seenModule[module]; ok {
				return nil, fmt.Errorf("模块 %q 同时声明在能力 %q 与 %q 中", module, prev, spec.Name)
			}
			seenModule[module] = spec.Name
		}
		for global := range spec.Globals {
			if prev, ok := seenGlobal[global]; ok {
				return nil, fmt.Errorf("全局函数 %q 同时声明在能力 %q 与 %q 中", global, prev, spec.Name)
			}
			seenGlobal[global] = spec.Name
		}
		for event, e := range spec.Events {
			key := e.Owner + "|" + event
			if prev, ok := seenEvent[key]; ok {
				return nil, fmt.Errorf("事件 %s.%s 同时声明在能力 %q 与 %q 中", e.Owner, event, prev, spec.Name)
			}
			seenEvent[key] = spec.Name
		}
		specs = append(specs, spec)
	}
	return newRegistry(specs), nil
}

func parseSpec(data []byte) (*Spec, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var spec Spec
	if err := dec.Decode(&spec); err != nil {
		return nil, fmt.Errorf("解析能力规格失败: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("能力规格只能包含一个 JSON 对象")
		}
		return nil, fmt.Errorf("解析能力规格失败: %w", err)
	}
	if err := validateSpec(&spec); err != nil {
		return nil, err
	}
	return &spec, nil
}
