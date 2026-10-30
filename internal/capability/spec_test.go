package capability

import (
	"strings"
	"testing"
)

func TestLoadBuiltin(t *testing.T) {
	reg, err := LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Specs()) != 4 {
		t.Fatalf("specs = %d, want 4", len(reg.Specs()))
	}
	core, ok := reg.Spec("core")
	if !ok {
		t.Fatal("missing core spec")
	}
	if core.Kind != KindCore || core.Installer != "LuaApis" || core.RequiresLua {
		t.Fatalf("unexpected core spec: %+v", core)
	}
	if len(core.Modules) != 10 {
		t.Fatalf("core modules = %d, want 10", len(core.Modules))
	}
	for _, name := range []string{"app", "bridge", "device", "file", "http", "json", "permission", "store", "ui", "web"} {
		if _, ok := core.Modules[name]; !ok {
			t.Fatalf("missing core module %q", name)
		}
	}
	if _, ok := core.Globals["log"]; !ok {
		t.Fatal("missing log global")
	}
	for _, event := range []string{"boot", "create", "pause", "resume", "destroy", "back", "newIntent"} {
		if core.Events[event].Owner != "app" {
			t.Fatalf("event %q owner = %q", event, core.Events[event].Owner)
		}
	}
	caps := reg.Capabilities()
	if len(caps) != 3 || caps[0] != "native" || caps[1] != "notify" || caps[2] != "task" {
		t.Fatalf("capabilities = %v", caps)
	}
	native, ok := reg.Spec("native")
	if !ok {
		t.Fatal("missing native spec")
	}
	if native.Kind != KindCapability || native.Installer != "NativeModule" || !native.RequiresLua {
		t.Fatalf("unexpected native spec: %+v", native)
	}
	if len(native.Permissions) != 0 || len(native.Components) != 0 || len(native.Resources) != 0 {
		t.Fatalf("native spec should not add permissions/components/resources: %+v", native)
	}
	if _, ok := native.Modules["native"].Functions["list"]; !ok {
		t.Fatal("native missing list function")
	}
	if NativeABIVersion != 1 {
		t.Fatalf("NativeABIVersion = %d", NativeABIVersion)
	}
	notify, ok := reg.Spec("notify")
	if !ok {
		t.Fatal("missing notify spec")
	}
	if notify.Kind != KindCapability || notify.Installer != "NotifyModule" || !notify.RequiresLua {
		t.Fatalf("unexpected notify spec: %+v", notify)
	}
	if len(notify.Permissions) != 1 || notify.Permissions[0] != "android.permission.POST_NOTIFICATIONS" {
		t.Fatalf("notify permissions = %v", notify.Permissions)
	}
	if len(notify.Resources) != 1 || notify.Resources[0] != "notification_icon" {
		t.Fatalf("notify resources = %v", notify.Resources)
	}
	if notify.Events["notification"].Owner != "app" || !notify.Events["notification"].Foreground {
		t.Fatalf("notify event = %+v", notify.Events["notification"])
	}
	for _, fn := range []string{"show", "cancel", "cancelAll", "enabled", "channel"} {
		if _, ok := notify.Modules["notify"].Functions[fn]; !ok {
			t.Fatalf("notify missing function %q", fn)
		}
	}

	task, ok := reg.Spec("task")
	if !ok {
		t.Fatal("missing task spec")
	}
	if task.Kind != KindCapability || task.Installer != "TaskModule" || !task.RequiresLua {
		t.Fatalf("unexpected task spec: %+v", task)
	}
	if len(task.Permissions) != 2 ||
		task.Permissions[0] != "android.permission.RECEIVE_BOOT_COMPLETED" ||
		task.Permissions[1] != "android.permission.SCHEDULE_EXACT_ALARM" {
		t.Fatalf("task permissions = %v", task.Permissions)
	}
	if len(task.Components) != 3 {
		t.Fatalf("task components = %v", task.Components)
	}
	if task.Events["task"].Owner != "app" || task.Events["task"].Foreground {
		t.Fatalf("task event = %+v", task.Events["task"])
	}
	for _, fn := range []string{"schedule", "cancel", "list", "exact"} {
		if _, ok := task.Modules["task"].Functions[fn]; !ok {
			t.Fatalf("task missing function %q", fn)
		}
	}
}

func TestParseSpecRejectsUnknownField(t *testing.T) {
	body := `{"schema":1,"name":"x","kind":"capability","summary":"s","installer":"X","typo":true,
	  "modules":{"x":{"functions":{"f":{"doc":"d"}}}}}`
	if _, err := parseSpec([]byte(body)); err == nil {
		t.Fatal("expected unknown field rejection")
	}
}

func TestParseSpecRejectsTrailingObject(t *testing.T) {
	body := `{"schema":1,"name":"x","kind":"capability","summary":"s","installer":"X",
	  "modules":{"x":{"functions":{"f":{"doc":"d"}}}}}{}`
	if _, err := parseSpec([]byte(body)); err == nil {
		t.Fatal("expected trailing object rejection")
	}
}

func baseSpec() *Spec {
	return &Spec{
		Schema:    Schema,
		Name:      "notify",
		Kind:      KindCapability,
		Summary:   "本地通知",
		Installer: "NotifyModule",
		Modules: map[string]Module{
			"notify": {Functions: map[string]Function{
				"show": {Doc: "显示通知。"},
			}},
		},
	}
}

func TestValidateSpec(t *testing.T) {
	if err := validateSpec(baseSpec()); err != nil {
		t.Fatalf("base spec should be valid: %v", err)
	}
	cases := map[string]func(*Spec){
		"schema":     func(s *Spec) { s.Schema = 2 },
		"name":       func(s *Spec) { s.Name = "Notify" },
		"kind":       func(s *Spec) { s.Kind = "plugin" },
		"summary":    func(s *Spec) { s.Summary = " " },
		"installer":  func(s *Spec) { s.Installer = "notify" },
		"permission": func(s *Spec) { s.Permissions = []string{"POST_NOTIFICATIONS"} },
		"component":  func(s *Spec) { s.Components = []string{"unknown"} },
		"resource":   func(s *Spec) { s.Resources = []string{"unknown"} },
		"module name": func(s *Spec) {
			s.Modules = map[string]Module{"Notify": {Functions: map[string]Function{"f": {Doc: "d"}}}}
		},
		"browser": func(s *Spec) {
			s.Modules = map[string]Module{"notify": {Browser: "native", Functions: map[string]Function{"f": {Doc: "d"}}}}
		},
		"no functions": func(s *Spec) { s.Modules = map[string]Module{"notify": {}} },
		"function": func(s *Spec) {
			s.Modules = map[string]Module{"notify": {Functions: map[string]Function{"bad name": {Doc: "d"}}}}
		},
		"function doc": func(s *Spec) { s.Modules = map[string]Module{"notify": {Functions: map[string]Function{"f": {}}}} },
		"global":       func(s *Spec) { s.Globals = map[string]Function{"bad name": {Doc: "d"}} },
		"event owner":  func(s *Spec) { s.Events = map[string]Event{"x": {Owner: "ui", Doc: "d"}} },
		"event doc":    func(s *Spec) { s.Events = map[string]Event{"x": {Owner: "app"}} },
		"empty": func(s *Spec) {
			s.Modules = nil
			s.Globals = nil
			s.Events = nil
		},
	}
	for name, mutate := range cases {
		spec := baseSpec()
		mutate(spec)
		if err := validateSpec(spec); err == nil {
			t.Fatalf("%s: expected validation error", name)
		}
	}
}

func TestLoadBuiltinDeterministic(t *testing.T) {
	first, err := LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(first.Names(), ",") != strings.Join(second.Names(), ",") {
		t.Fatalf("names differ: %v != %v", first.Names(), second.Names())
	}
	if strings.Join(first.Capabilities(), ",") != strings.Join(second.Capabilities(), ",") {
		t.Fatal("capabilities are not deterministic")
	}
}

func coreOnlyRegistry(t *testing.T) *Registry {
	t.Helper()
	reg := testRegistry(t)
	return newRegistry([]*Spec{reg.specs["core"]})
}
