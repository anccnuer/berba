package capability

import (
	"encoding/json"
	"strings"
	"testing"
)

func testRegistry(t *testing.T) *Registry {
	t.Helper()
	core, err := LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}
	return core
}

func TestResolveCore(t *testing.T) {
	reg := testRegistry(t)
	res, err := reg.Resolve(Request{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Capabilities) != 0 {
		t.Fatalf("capabilities = %v", res.Capabilities)
	}
	if !res.Has("core") || res.Has("notify") {
		t.Fatalf("unexpected enabled set: %v", res.enabled)
	}
	if len(res.Modules) != 10 {
		t.Fatalf("modules = %d, want 10", len(res.Modules))
	}
	if res.RequiresLua {
		t.Fatal("core should not require Lua")
	}
	if len(res.Permissions) != 0 || len(res.Components) != 0 || len(res.Resources) != 0 {
		t.Fatalf("unexpected auto additions: %+v", res)
	}
	if len(res.Installers) != 1 || res.Installers[0] != "LuaApis" {
		t.Fatalf("installers = %v", res.Installers)
	}
	app, web := 0, 0
	for _, e := range res.Events {
		switch e.Owner {
		case "app":
			app++
		case "web":
			web++
		}
	}
	if app != 7 || web != 3 {
		t.Fatalf("events app=%d web=%d", app, web)
	}
	for i := 1; i < len(res.Events); i++ {
		if res.Events[i-1].Owner > res.Events[i].Owner {
			t.Fatalf("events are not grouped: %+v", res.Events)
		}
	}
}

func TestResolveUnknownAndCore(t *testing.T) {
	reg := testRegistry(t)
	if _, err := reg.Resolve(Request{Capabilities: []string{"missing"}}); err == nil {
		t.Fatal("expected unknown capability error")
	}
	if _, err := reg.Resolve(Request{Capabilities: []string{"core"}}); err == nil {
		t.Fatal("core must not be listed in capabilities")
	}
	if _, err := reg.Resolve(Request{Capabilities: []string{" "}}); err == nil {
		t.Fatal("expected empty capability error")
	}
}

func TestResolveCapability(t *testing.T) {
	core, err := LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}
	notify := baseSpec()
	notify.RequiresLua = true
	notify.Permissions = []string{"android.permission.POST_NOTIFICATIONS"}
	notify.Components = []string{"task_job_service"}
	notify.Resources = []string{"notification_icon"}
	notify.Modules = map[string]Module{
		"notify": {Browser: "partial", Functions: map[string]Function{
			"show":    {Doc: "显示通知。"},
			"enabled": {Doc: "通知是否可用。"},
		}},
	}
	notify.Events = map[string]Event{
		"notification": {Owner: "app", Foreground: true, Doc: "用户点击通知。"},
	}
	reg := newRegistry([]*Spec{core.specs["core"], notify})

	res, err := reg.Resolve(Request{Capabilities: []string{"notify", "notify"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Capabilities) != 1 || res.Capabilities[0] != "notify" {
		t.Fatalf("capabilities = %v", res.Capabilities)
	}
	if !res.Has("notify") || !res.Has("core") {
		t.Fatal("notify should be enabled")
	}
	if !res.RequiresLua {
		t.Fatal("notify should require Lua")
	}
	if len(res.Modules) != 11 {
		t.Fatalf("modules = %d, want 11", len(res.Modules))
	}
	if len(res.Permissions) != 1 || res.Permissions[0] != "android.permission.POST_NOTIFICATIONS" {
		t.Fatalf("permissions = %v", res.Permissions)
	}
	if len(res.Components) != 1 || res.Components[0] != "task_job_service" {
		t.Fatalf("components = %v", res.Components)
	}
	if len(res.Resources) != 1 || res.Resources[0] != "notification_icon" {
		t.Fatalf("resources = %v", res.Resources)
	}
	if len(res.Installers) != 2 || res.Installers[0] != "LuaApis" || res.Installers[1] != "NotifyModule" {
		t.Fatalf("installers = %v", res.Installers)
	}
	found := false
	for _, e := range res.Events {
		if e.Owner == "app" && e.Name == "notification" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing notification event")
	}
}

func TestResolveRejectsDuplicateModule(t *testing.T) {
	core, err := LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}
	a := baseSpec()
	a.Name = "aa"
	a.Installer = "AaModule"
	b := baseSpec()
	b.Name = "bb"
	b.Installer = "BbModule"
	reg := newRegistry([]*Spec{core.specs["core"], a, b})
	if _, err := reg.Resolve(Request{Capabilities: []string{"aa", "bb"}}); err == nil {
		t.Fatal("expected duplicate module error")
	}
}

func TestRuntimeJSON(t *testing.T) {
	reg := testRegistry(t)
	res, err := reg.Resolve(Request{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := res.RuntimeJSON()
	if err != nil {
		t.Fatal(err)
	}
	var asset struct {
		Schema       int                        `json:"schema"`
		Capabilities []string                   `json:"capabilities"`
		Modules      map[string]json.RawMessage `json:"modules"`
		Globals      map[string]json.RawMessage `json:"globals"`
		Events       map[string]json.RawMessage `json:"events"`
		Native       struct {
			ABIVersion int               `json:"abiVersion"`
			Plugins    []json.RawMessage `json:"plugins"`
		} `json:"native"`
	}
	if err := json.Unmarshal(data, &asset); err != nil {
		t.Fatal(err)
	}
	if asset.Schema != Schema || len(asset.Capabilities) != 0 {
		t.Fatalf("asset header = %+v", asset)
	}
	if len(asset.Modules) != 10 {
		t.Fatalf("modules = %d", len(asset.Modules))
	}
	if _, ok := asset.Globals["log"]; !ok {
		t.Fatal("missing log global")
	}
	if len(asset.Events) != 2 {
		t.Fatalf("events = %d", len(asset.Events))
	}
	if asset.Native.ABIVersion != NativeABIVersion || asset.Native.Plugins == nil {
		t.Fatalf("native = %+v", asset.Native)
	}
	if strings.Contains(string(data), `"show"`) {
		t.Fatal("core-only asset must not contain capability functions")
	}
	again, err := res.RuntimeJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(data) {
		t.Fatal("runtime asset is not deterministic")
	}
}

func TestRuntimeJSONNativePlugins(t *testing.T) {
	reg := testRegistry(t)
	res, err := reg.Resolve(Request{Capabilities: []string{"native"}})
	if err != nil {
		t.Fatal(err)
	}
	res.SetNativePlugins([]ResolvedNativePlugin{
		{
			Name:    "zeta",
			LibName: "libberba_zeta.so",
			Config:  `{"b":2,"a":1}`,
			Modules: []ResolvedNativeModule{{Name: "zmod", Functions: []ResolvedNativeFunction{{Name: "call"}}}},
		},
		{
			Name:    "echo",
			LibName: "libberba_echo.so",
			Config:  "{}",
			Modules: []ResolvedNativeModule{{
				Name: "echo",
				Functions: []ResolvedNativeFunction{
					{Name: "run", Async: true},
					{Name: "call"},
				},
			}},
		},
	})
	if !res.ExtractNativeLibs {
		t.Fatal("native plugins must require extractNativeLibs")
	}
	if res.NativePlugins[0].Name != "echo" || res.NativePlugins[0].Modules[0].Functions[0].Name != "call" {
		t.Fatalf("plugins not normalized: %+v", res.NativePlugins)
	}
	data, err := res.RuntimeJSON()
	if err != nil {
		t.Fatal(err)
	}
	var asset struct {
		Native struct {
			ABIVersion int `json:"abiVersion"`
			Plugins    []struct {
				Name    string          `json:"name"`
				LibName string          `json:"libName"`
				Config  json.RawMessage `json:"config"`
				Modules map[string]struct {
					Functions map[string]struct {
						Async bool `json:"async"`
					} `json:"functions"`
				} `json:"modules"`
			} `json:"plugins"`
		} `json:"native"`
	}
	if err := json.Unmarshal(data, &asset); err != nil {
		t.Fatal(err)
	}
	if asset.Native.ABIVersion != 1 || len(asset.Native.Plugins) != 2 {
		t.Fatalf("native = %+v", asset.Native)
	}
	echo := asset.Native.Plugins[0]
	if echo.Name != "echo" || echo.LibName != "libberba_echo.so" || string(echo.Config) != "{}" {
		t.Fatalf("echo plugin = %+v", echo)
	}
	if !echo.Modules["echo"].Functions["run"].Async || echo.Modules["echo"].Functions["call"].Async {
		t.Fatalf("echo functions = %+v", echo.Modules["echo"].Functions)
	}
	zeta := asset.Native.Plugins[1]
	var zetaConfig map[string]int
	if err := json.Unmarshal(zeta.Config, &zetaConfig); err != nil {
		t.Fatal(err)
	}
	if zetaConfig["a"] != 1 || zetaConfig["b"] != 2 {
		t.Fatalf("zeta config = %s", zeta.Config)
	}

	res.SetNativePlugins(nil)
	if res.ExtractNativeLibs {
		t.Fatal("clearing plugins must reset extractNativeLibs")
	}
}
