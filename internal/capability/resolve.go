package capability

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Resolve 合并 core 与请求启用的能力，输出确定性的权限、组件、模块与事件集合。
func (r *Registry) Resolve(req Request) (*Resolved, error) {
	requested := make([]string, 0, len(req.Capabilities))
	seen := map[string]bool{}
	for _, raw := range req.Capabilities {
		name := strings.TrimSpace(raw)
		if name == "" {
			return nil, fmt.Errorf("capabilities 中存在空项")
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		spec, ok := r.specs[name]
		if !ok {
			return nil, fmt.Errorf("未知能力: %q", name)
		}
		if spec.Kind != KindCapability {
			return nil, fmt.Errorf("能力 %q 始终启用，无需在 capabilities 中声明", name)
		}
		requested = append(requested, name)
	}
	sort.Strings(requested)

	modules := map[string]*ResolvedModule{}
	events := map[string]map[string]ResolvedEvent{}
	enabled := map[string]bool{}
	var globals []ResolvedFunction
	var permissions, components, resources, installers []string
	requiresLua := false

	addEvents := func(spec *Spec) error {
		for name, e := range spec.Events {
			if events[e.Owner] == nil {
				events[e.Owner] = map[string]ResolvedEvent{}
			}
			if _, ok := events[e.Owner][name]; ok {
				return fmt.Errorf("事件 %s.%s 重复", e.Owner, name)
			}
			events[e.Owner][name] = ResolvedEvent{
				Owner: e.Owner, Name: name, Foreground: e.Foreground, Doc: e.Doc,
			}
		}
		return nil
	}

	addSpec := func(spec *Spec) error {
		if enabled[spec.Name] {
			return nil
		}
		enabled[spec.Name] = true
		for name, m := range spec.Modules {
			if _, ok := modules[name]; ok {
				return fmt.Errorf("模块 %q 重复", name)
			}
			resolved := &ResolvedModule{Name: name, Browser: m.Browser}
			for fn, f := range m.Functions {
				resolved.Functions = append(resolved.Functions, ResolvedFunction{
					Name: fn, Async: f.Async, Foreground: f.Foreground, Doc: f.Doc,
				})
			}
			sort.Slice(resolved.Functions, func(i, j int) bool {
				return resolved.Functions[i].Name < resolved.Functions[j].Name
			})
			modules[name] = resolved
		}
		for name, f := range spec.Globals {
			globals = append(globals, ResolvedFunction{
				Name: name, Async: f.Async, Foreground: f.Foreground, Doc: f.Doc,
			})
		}
		permissions = append(permissions, spec.Permissions...)
		components = append(components, spec.Components...)
		resources = append(resources, spec.Resources...)
		installers = append(installers, spec.Installer)
		requiresLua = requiresLua || spec.RequiresLua
		return addEvents(spec)
	}

	for _, spec := range r.Specs() {
		if spec.Kind != KindCore {
			continue
		}
		if err := addSpec(spec); err != nil {
			return nil, err
		}
	}
	for _, name := range requested {
		if err := addSpec(r.specs[name]); err != nil {
			return nil, err
		}
	}

	moduleList := make([]ResolvedModule, 0, len(modules))
	for _, m := range modules {
		moduleList = append(moduleList, *m)
	}
	sort.Slice(moduleList, func(i, j int) bool { return moduleList[i].Name < moduleList[j].Name })
	sort.Slice(globals, func(i, j int) bool { return globals[i].Name < globals[j].Name })

	eventList := make([]ResolvedEvent, 0)
	for _, owner := range []string{"app", "web"} {
		byName := events[owner]
		names := make([]string, 0, len(byName))
		for name := range byName {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			eventList = append(eventList, byName[name])
		}
	}
	for owner := range events {
		if owner != "app" && owner != "web" {
			return nil, fmt.Errorf("未知事件归属: %q", owner)
		}
	}

	res := &Resolved{
		Capabilities: requested,
		Modules:      moduleList,
		Globals:      globals,
		Events:       eventList,
		Permissions:  sortUnique(permissions),
		Components:   sortUnique(components),
		Resources:    sortUnique(resources),
		Installers:   sortUnique(installers),
		RequiresLua:  requiresLua,
		enabled:      enabled,
	}
	return res, nil
}

type runtimeAsset struct {
	Schema       int                                `json:"schema"`
	Capabilities []string                           `json:"capabilities"`
	Modules      map[string]runtimeModule           `json:"modules"`
	Globals      map[string]runtimeFunction         `json:"globals"`
	Events       map[string]map[string]runtimeEvent `json:"events"`
	Native       runtimeNative                      `json:"native"`
}

type runtimeModule struct {
	Browser   string                     `json:"browser,omitempty"`
	Functions map[string]runtimeFunction `json:"functions"`
}

type runtimeFunction struct {
	Async      bool `json:"async"`
	Foreground bool `json:"foreground"`
}

type runtimeEvent struct {
	Foreground bool `json:"foreground"`
}

type runtimeNative struct {
	ABIVersion int             `json:"abiVersion"`
	Plugins    []runtimePlugin `json:"plugins"`
}

type runtimePlugin struct {
	Name    string                   `json:"name"`
	LibName string                   `json:"libName"`
	Config  json.RawMessage          `json:"config"`
	Modules map[string]runtimeModule `json:"modules"`
}

// RuntimeJSON 生成 assets/berba_capabilities.json 的内容，不含文档字符串。
func (r *Resolved) RuntimeJSON() ([]byte, error) {
	asset := runtimeAsset{
		Schema:       Schema,
		Capabilities: append([]string{}, r.Capabilities...),
		Modules:      make(map[string]runtimeModule, len(r.Modules)),
		Globals:      make(map[string]runtimeFunction, len(r.Globals)),
		Events:       map[string]map[string]runtimeEvent{"app": {}, "web": {}},
		Native:       runtimeNative{ABIVersion: NativeABIVersion, Plugins: []runtimePlugin{}},
	}
	for _, m := range r.Modules {
		rm := runtimeModule{Browser: m.Browser, Functions: map[string]runtimeFunction{}}
		for _, f := range m.Functions {
			rm.Functions[f.Name] = runtimeFunction{Async: f.Async, Foreground: f.Foreground}
		}
		asset.Modules[m.Name] = rm
	}
	for _, f := range r.Globals {
		asset.Globals[f.Name] = runtimeFunction{Async: f.Async, Foreground: f.Foreground}
	}
	for _, e := range r.Events {
		asset.Events[e.Owner][e.Name] = runtimeEvent{Foreground: e.Foreground}
	}
	for _, p := range r.NativePlugins {
		rp := runtimePlugin{
			Name:    p.Name,
			LibName: p.LibName,
			Config:  json.RawMessage("{}"),
			Modules: make(map[string]runtimeModule, len(p.Modules)),
		}
		if strings.TrimSpace(p.Config) != "" {
			rp.Config = json.RawMessage(p.Config)
		}
		for _, m := range p.Modules {
			rm := runtimeModule{Functions: make(map[string]runtimeFunction, len(m.Functions))}
			for _, f := range m.Functions {
				rm.Functions[f.Name] = runtimeFunction{Async: f.Async}
			}
			rp.Modules[m.Name] = rm
		}
		asset.Native.Plugins = append(asset.Native.Plugins, rp)
	}
	data, err := json.MarshalIndent(asset, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
