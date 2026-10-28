package config

import "encoding/json"

type AssetConfig struct {
	Name           string   `json:"name"`
	Package        string   `json:"package"`
	VersionName    string   `json:"versionName"`
	VersionCode    int      `json:"versionCode"`
	StartURL       string   `json:"startUrl"`
	SPA            bool     `json:"spa"`
	AllowedOrigins []string `json:"allowedOrigins"`
	LuaEntry       string   `json:"luaEntry,omitempty"`
	Cleartext      bool     `json:"cleartext"`
	Debug          bool     `json:"debug"`
	Theme          string   `json:"theme"`
}

// AssetConfig 生成写入 assets/app_config.json 的运行时配置。
func (p *Project) AssetConfig(debug bool) ([]byte, error) {
	ac := AssetConfig{
		Name:           p.Config.Name,
		Package:        p.Config.Package,
		VersionName:    p.Config.VersionName,
		VersionCode:    p.Config.VersionCode,
		StartURL:       p.Config.Web.StartURL,
		SPA:            p.Config.Web.SPA,
		AllowedOrigins: p.AllowedOrigins,
		Cleartext:      p.Config.Cleartext,
		Debug:          debug || p.Config.Debug,
		Theme:          p.Config.Theme,
	}
	if p.HasLua {
		entry := p.Config.Lua.Entry
		if entry == "" {
			entry = "main.lua"
		}
		ac.LuaEntry = entry
	}
	return json.MarshalIndent(ac, "", "  ")
}
