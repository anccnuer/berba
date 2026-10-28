package manifest

import (
	"strings"
	"testing"

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
