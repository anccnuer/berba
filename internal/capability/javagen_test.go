package capability

import (
	"bytes"
	"strings"
	"testing"
)

func TestGenerateJavaRegistry(t *testing.T) {
	reg := coreOnlyRegistry(t)
	first, err := GenerateJavaRegistry(reg)
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateJavaRegistry(reg)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("java registry is not deterministic")
	}
	out := string(first)
	for _, want := range []string{
		"package com.berba.shell;",
		"final class CapabilityRegistry",
		"static boolean isImplemented(String capability)",
		"static void install(LuaRuntime rt, Globals g, CapabilityConfig cfg)",
		"LuaApis.install(rt, g); // core",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("java registry missing %q", want)
		}
	}
	if strings.Contains(out, "NotifyModule") {
		t.Fatal("stage 1 registry must not reference future installers")
	}
}

func TestGenerateJavaRegistryIncludesCapabilities(t *testing.T) {
	core, err := LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}
	notify := baseSpec()
	reg := newRegistry([]*Spec{core.specs["core"], notify})
	data, err := GenerateJavaRegistry(reg)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	for _, want := range []string{
		`case "notify":`,
		`if (cfg.hasCapability("notify")) {`,
		"NotifyModule.install(rt, g); // 能力 notify",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
}
