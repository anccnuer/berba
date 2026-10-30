package manifest

import (
	"bytes"
	"strings"
	"testing"

	"berba/internal/capability"
	"berba/internal/config"
)

func testProject() *config.Project {
	return &config.Project{
		Config: config.Config{
			Package:     "com.example.demo",
			VersionName: "1.0.0",
			VersionCode: 3,
			Theme:       "dark",
			Cleartext:   true,
			Orientation: "landscape",
			MetaData:    map[string]string{"channel": "a&b"},
		},
		Permissions: []string{"android.permission.CAMERA", "android.permission.INTERNET"},
	}
}

func TestRenderBasic(t *testing.T) {
	data, err := Render(testProject(), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(data); err != nil {
		t.Fatal(err)
	}
	out := string(data)
	checks := []string{
		`package="com.example.demo"`,
		`android:versionCode="3"`,
		`android:versionName="1.0.0"`,
		`android:minSdkVersion="24"`,
		`android:targetSdkVersion="35"`,
		`android:name="android.permission.CAMERA"`,
		`android:name="android.permission.INTERNET"`,
		`android:theme="@android:style/Theme.Material.NoActionBar"`,
		`android:usesCleartextTraffic="true"`,
		`android:screenOrientation="landscape"`,
		`android:name="com.berba.shell.MainActivity"`,
		`android:label="@string/app_name"`,
		`android:icon="@mipmap/ic_launcher"`,
		`android:value="a&amp;b"`,
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("manifest missing %s\n%s", want, out)
		}
	}
	if strings.Contains(out, "debuggable") {
		t.Fatal("release manifest must not be debuggable")
	}
}

func TestRenderDebug(t *testing.T) {
	data, err := Render(testProject(), true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `android:debuggable="true"`) {
		t.Fatal("debug manifest should be debuggable")
	}
}

func TestRenderAutoOrientation(t *testing.T) {
	p := testProject()
	p.Config.Orientation = "auto"
	data, err := Render(p, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "screenOrientation") {
		t.Fatal("auto orientation should not pin screenOrientation")
	}
}

func TestRenderDeterministic(t *testing.T) {
	p := testProject()
	p.Config.MetaData = map[string]string{"z": "1", "a": "2", "m": "3"}
	first, err := Render(p, false)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		next, err := Render(p, false)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, next) {
			t.Fatal("manifest output is not deterministic")
		}
	}
	out := string(first)
	ia := strings.Index(out, `<meta-data android:name="a"`)
	im := strings.Index(out, `<meta-data android:name="m"`)
	iz := strings.Index(out, `<meta-data android:name="z"`)
	if ia < 0 || im < 0 || iz < 0 || !(ia < im && im < iz) {
		t.Fatalf("meta-data is not sorted: a=%d m=%d z=%d", ia, im, iz)
	}
}

func TestRenderDeepLinks(t *testing.T) {
	p := testProject()
	p.DeepLinks = []config.DeepLink{{Scheme: "myapp", Host: "open"}, {Scheme: "notes", Host: ""}}
	data, err := Render(p, false)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	for _, want := range []string{
		`android:launchMode="singleTask"`,
		`<action android:name="android.intent.action.VIEW"/>`,
		`android:scheme="myapp" android:host="open"`,
		`android:scheme="notes"/>`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("manifest missing %s\n%s", want, out)
		}
	}

	plain, err := Render(testProject(), false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "singleTask") || strings.Contains(string(plain), "intent.action.VIEW") {
		t.Fatalf("无深链时不应出现深链配置:\n%s", plain)
	}
}

func TestRenderCapabilityAdditions(t *testing.T) {
	p := testProject()
	res := &capability.Resolved{
		Capabilities:      []string{"notify", "task"},
		Permissions:       []string{"android.permission.POST_NOTIFICATIONS"},
		Components:        []string{"task_job_service", "task_boot_receiver"},
		ExtractNativeLibs: true,
	}
	data, err := RenderResolved(p, false, res)
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(data); err != nil {
		t.Fatal(err)
	}
	out := string(data)
	for _, want := range []string{
		`android:name="android.permission.POST_NOTIFICATIONS"`,
		`android:name="com.berba.shell.TaskJobService"`,
		`android:permission="android.permission.BIND_JOB_SERVICE"`,
		`android:name="com.berba.shell.TaskBootReceiver"`,
		`android:name="android.intent.action.BOOT_COMPLETED"`,
		`android:launchMode="singleTask"`,
		`android:extractNativeLibs="true"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("manifest missing %s\n%s", want, out)
		}
	}
	perm := strings.Index(out, "android.permission.CAMERA")
	capPerm := strings.Index(out, "android.permission.POST_NOTIFICATIONS")
	internet := strings.Index(out, "android.permission.INTERNET")
	if !(perm < capPerm && capPerm < internet) {
		t.Fatalf("permission order is wrong: camera=%d cap=%d internet=%d", perm, capPerm, internet)
	}
}

func TestRenderCapabilityUnknownComponent(t *testing.T) {
	_, err := RenderResolved(testProject(), false, &capability.Resolved{
		Components: []string{"bogus"},
	})
	if err == nil {
		t.Fatal("expected unknown component error")
	}
}

func TestValidateRejectsMalformedXML(t *testing.T) {
	if err := Validate([]byte(`<manifest><application></manifest>`)); err == nil {
		t.Fatal("expected malformed XML rejection")
	}
	if err := Validate([]byte(`<html></html>`)); err == nil {
		t.Fatal("expected non-manifest root rejection")
	}
}

func TestThemeValues(t *testing.T) {
	cases := map[string]string{
		"light":  "@android:style/Theme.Material.Light.NoActionBar",
		"dark":   "@android:style/Theme.Material.NoActionBar",
		"device": "@android:style/Theme.DeviceDefault.NoActionBar",
	}
	for in, want := range cases {
		if got := ThemeValue(in); got != want {
			t.Fatalf("ThemeValue(%q) = %q, want %q", in, got, want)
		}
	}
}
