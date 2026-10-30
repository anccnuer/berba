package manifest

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"sort"
	"text/template"

	"berba/internal/capability"
	"berba/internal/config"
)

const minSDK = 24
const targetSDK = 35

type MetaData struct {
	Name  string
	Value string
}

type DeepLink struct {
	Scheme string
	Host   string
}

type Data struct {
	Package           string
	VersionName       string
	VersionCode       int
	MinSDK            int
	TargetSDK         int
	Theme             string
	Cleartext         bool
	Debuggable        bool
	ScreenOrientation string
	Permissions       []string
	MetaData          []MetaData
	DeepLinks         []DeepLink
	Components        []string
	SingleTask        bool
	ExtractNativeLibs bool
}

const manifestTemplate = `<manifest xmlns:android="http://schemas.android.com/apk/res/android"
    package="{{.Package}}"
    android:versionCode="{{.VersionCode}}"
    android:versionName="{{.VersionName}}">
    <uses-sdk android:minSdkVersion="{{.MinSDK}}" android:targetSdkVersion="{{.TargetSDK}}"/>
{{- range .Permissions}}
    <uses-permission android:name="{{.}}"/>
{{- end}}
    <application
        android:label="@string/app_name"
        android:icon="@mipmap/ic_launcher"
        android:theme="{{.Theme}}"
        android:allowBackup="false"
        android:supportsRtl="true"
        android:hardwareAccelerated="true"
        android:usesCleartextTraffic="{{.Cleartext}}"
        android:enableOnBackInvokedCallback="true"{{if .ExtractNativeLibs}}
        android:extractNativeLibs="true"{{end}}{{if .Debuggable}}
        android:debuggable="true"{{end}}>
        <activity
            android:name="com.berba.shell.MainActivity"
            android:exported="true"{{if .ScreenOrientation}}
            android:screenOrientation="{{.ScreenOrientation}}"{{end}}
            android:configChanges="orientation|screenSize|smallestScreenSize|screenLayout|keyboardHidden|uiMode"
            android:windowSoftInputMode="adjustResize"{{if .SingleTask}}
            android:launchMode="singleTask"{{end}}>
            <intent-filter>
                <action android:name="android.intent.action.MAIN"/>
                <category android:name="android.intent.category.LAUNCHER"/>
            </intent-filter>
{{- range .DeepLinks}}
            <intent-filter>
                <action android:name="android.intent.action.VIEW"/>
                <category android:name="android.intent.category.DEFAULT"/>
                <category android:name="android.intent.category.BROWSABLE"/>
                <data android:scheme="{{.Scheme}}"{{if .Host}} android:host="{{.Host}}"{{end}}/>
            </intent-filter>
{{- end}}
        </activity>
{{- range .Components}}
        {{.}}
{{- end}}
{{- range .MetaData}}
        <meta-data android:name="{{.Name}}" android:value="{{.Value}}"/>
{{- end}}
    </application>
</manifest>
`

func Render(p *config.Project, debug bool) ([]byte, error) {
	return RenderResolved(p, debug, nil)
}

// RenderResolved 渲染 Manifest，并合并能力带来的权限与组件。
func RenderResolved(p *config.Project, debug bool, res *capability.Resolved) ([]byte, error) {
	data := Data{
		Package:     p.Config.Package,
		VersionName: html.EscapeString(p.Config.VersionName),
		VersionCode: p.Config.VersionCode,
		MinSDK:      minSDK,
		TargetSDK:   targetSDK,
		Theme:       ThemeValue(p.Config.Theme),
		Cleartext:   p.Config.Cleartext,
		Debuggable:  debug,
		Permissions: p.Permissions,
	}
	if res != nil {
		data.Permissions = mergePermissions(p.Permissions, res.Permissions)
		data.ExtractNativeLibs = res.ExtractNativeLibs
		for _, name := range res.Components {
			component, ok := componentTemplates[name]
			if !ok {
				return nil, fmt.Errorf("未知的 Manifest 组件模板: %q", name)
			}
			data.Components = append(data.Components, component)
		}
	}
	if p.Config.Orientation != "auto" {
		data.ScreenOrientation = p.Config.Orientation
	}
	keys := make([]string, 0, len(p.Config.MetaData))
	for k := range p.Config.MetaData {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		data.MetaData = append(data.MetaData, MetaData{
			Name:  html.EscapeString(k),
			Value: html.EscapeString(p.Config.MetaData[k]),
		})
	}

	for _, link := range p.DeepLinks {
		data.DeepLinks = append(data.DeepLinks, DeepLink{
			Scheme: html.EscapeString(link.Scheme),
			Host:   html.EscapeString(link.Host),
		})
	}
	data.SingleTask = len(data.DeepLinks) > 0 || (res != nil && res.Has("notify"))

	tmpl, err := template.New("manifest").Parse(manifestTemplate)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// mergePermissions 保持「用户权限 → 能力权限（排序）→ INTERNET」的顺序。
func mergePermissions(base, extra []string) []string {
	out := make([]string, 0, len(base)+len(extra)+1)
	seen := map[string]bool{}
	add := func(perm string) {
		if perm == "" || perm == "android.permission.INTERNET" || seen[perm] {
			return
		}
		seen[perm] = true
		out = append(out, perm)
	}
	for _, perm := range base {
		add(perm)
	}
	for _, perm := range extra {
		add(perm)
	}
	out = append(out, "android.permission.INTERNET")
	return out
}

var componentTemplates = map[string]string{
	"task_job_service":    `<service android:name="com.berba.shell.TaskJobService" android:exported="true" android:permission="android.permission.BIND_JOB_SERVICE"/>`,
	"task_alarm_receiver": `<receiver android:name="com.berba.shell.TaskAlarmReceiver" android:exported="false"/>`,
	"task_boot_receiver": `<receiver android:name="com.berba.shell.TaskBootReceiver" android:exported="false">` +
		`<intent-filter>` +
		`<action android:name="android.intent.action.BOOT_COMPLETED"/>` +
		`<action android:name="android.intent.action.MY_PACKAGE_REPLACED"/>` +
		`<action android:name="android.intent.action.TIME_SET"/>` +
		`<action android:name="android.intent.action.TIMEZONE_CHANGED"/>` +
		`<action android:name="android.app.action.SCHEDULE_EXACT_ALARM_PERMISSION_STATE_CHANGED"/>` +
		`</intent-filter></receiver>`,
}

func ThemeValue(theme string) string {
	switch theme {
	case "dark":
		return "@android:style/Theme.Material.NoActionBar"
	case "device":
		return "@android:style/Theme.DeviceDefault.NoActionBar"
	default:
		return "@android:style/Theme.Material.Light.NoActionBar"
	}
}

func Validate(data []byte) error {
	dec := xml.NewDecoder(bytes.NewReader(data))
	depth := 0
	rootSeen := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("渲染结果异常: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if depth == 0 {
				if t.Name.Local != "manifest" {
					return fmt.Errorf("渲染结果异常: 根元素必须是 manifest")
				}
				rootSeen = true
			}
			depth++
		case xml.EndElement:
			depth--
			if depth < 0 {
				return fmt.Errorf("渲染结果异常: 元素闭合不匹配")
			}
		}
	}
	if !rootSeen || depth != 0 {
		return fmt.Errorf("渲染结果异常: 不是完整的 manifest 文档")
	}
	return nil
}
