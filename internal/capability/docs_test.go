package capability

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderDocs(t *testing.T) {
	reg := coreOnlyRegistry(t)
	first, err := RenderDocs(reg)
	if err != nil {
		t.Fatal(err)
	}
	second, err := RenderDocs(reg)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("docs are not deterministic")
	}
	out := string(first)
	for _, want := range []string{
		"# 能力规格",
		"## core（core）",
		"| `app.on` |",
		"| `pageStarted` |",
		"| `log` |",
		"| `create` |",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("docs missing %q", want)
		}
	}
	if strings.Contains(out, "notification_icon") {
		t.Fatal("core docs should not mention capability resources")
	}
}
