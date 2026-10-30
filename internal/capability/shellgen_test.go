package capability

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestGenerateShellManifest(t *testing.T) {
	reg := coreOnlyRegistry(t)
	first, err := GenerateShellManifest(reg)
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateShellManifest(reg)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("shell manifest is not deterministic")
	}
	var doc struct {
		Schema       int      `json:"schema"`
		Capabilities []string `json:"capabilities"`
		NativeABI    int      `json:"nativeAbiVersion"`
	}
	if err := json.Unmarshal(first, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Schema != Schema || doc.NativeABI != NativeABIVersion {
		t.Fatalf("doc = %+v", doc)
	}
	if doc.Capabilities == nil || len(doc.Capabilities) != 0 {
		t.Fatalf("capabilities = %#v", doc.Capabilities)
	}
}

func TestGenerateShellManifestListsCapabilities(t *testing.T) {
	core, err := LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}
	reg := newRegistry([]*Spec{core.specs["core"], baseSpec()})
	data, err := GenerateShellManifest(reg)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Capabilities) != 1 || doc.Capabilities[0] != "notify" {
		t.Fatalf("capabilities = %v", doc.Capabilities)
	}
}
