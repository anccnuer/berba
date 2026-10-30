package capability

import "encoding/json"

type shellManifest struct {
	Schema           int      `json:"schema"`
	Capabilities     []string `json:"capabilities"`
	NativeABIVersion int      `json:"nativeAbiVersion"`
}

// GenerateShellManifest 生成 shell/shell.json，用于 payload 能力协商。
func GenerateShellManifest(r *Registry) ([]byte, error) {
	caps := r.Capabilities()
	if caps == nil {
		caps = []string{}
	}
	data, err := json.MarshalIndent(shellManifest{
		Schema:           Schema,
		Capabilities:     caps,
		NativeABIVersion: NativeABIVersion,
	}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
