package manifest

import (
	"bytes"
	"fmt"
	"html"
	"text/template"

	"guiapp/internal/config"
)

const minSDK = 24
const targetSDK = 35

type MetaData struct {
	Name  string
	Value string
}

type Data struct {
	Package           string
	VersionName       string
	VersionCode       int
	Theme             string
	Cleartext         bool
	Debuggable        bool
	ScreenOrientation string
	Permissions       []string
	MetaData          []MetaData
}

const manifestTemplate = `<manifest xmlns:android="http://schemas.android.com/apk/res/android"
    package="{{.Package}}"
    android:versionCode="{{.VersionCode}}"
    android:versionName="{{.VersionName}}">
    <uses-sdk android:minSdkVersion="24" android:targetSdkVersion="35"/>
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
        android:enableOnBackInvokedCallback="true"{{if .Debuggable}}
        android:debuggable="true"{{end}}>
        <activity
            android:name="com.guiapp.shell.MainActivity"
            android:exported="true"{{if .ScreenOrientation}}
            android:screenOrientation="{{.ScreenOrientation}}"{{end}}
            android:configChanges="orientation|screenSize|smallestScreenSize|screenLayout|keyboardHidden|uiMode"
            android:windowSoftInputMode="adjustResize">
            <intent-filter>
                <action android:name="android.intent.action.MAIN"/>
                <category android:name="android.intent.category.LAUNCHER"/>
            </intent-filter>
        </activity>
{{- range .MetaData}}
        <meta-data android:name="{{.Name}}" android:value="{{.Value}}"/>
{{- end}}
    </application>
</manifest>
`

func Render(p *config.Project, debug bool) ([]byte, error) {
	data := Data{
		Package:     p.Config.Package,
		VersionName: html.EscapeString(p.Config.VersionName),
		VersionCode: p.Config.VersionCode,
		Theme:       ThemeValue(p.Config.Theme),
		Cleartext:   p.Config.Cleartext,
		Debuggable:  debug,
		Permissions: p.Permissions,
	}
	if p.Config.Orientation != "auto" {
		data.ScreenOrientation = p.Config.Orientation
	}
	for k, v := range p.Config.MetaData {
		data.MetaData = append(data.MetaData, MetaData{
			Name:  html.EscapeString(k),
			Value: html.EscapeString(v),
		})
	}

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

func MinSDK() int    { return minSDK }
func TargetSDK() int { return targetSDK }

func Validate(data []byte) error {
	if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("<manifest")) {
		return fmt.Errorf("Manifest 渲染结果异常")
	}
	return nil
}
