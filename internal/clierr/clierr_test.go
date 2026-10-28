package clierr

import (
	"errors"
	"fmt"
	"testing"
)

func TestExitCode(t *testing.T) {
	if got := ExitCode(errors.New("plain")); got != 1 {
		t.Fatalf("plain = %d, want 1", got)
	}
	for _, tc := range []struct {
		code Code
		want int
	}{
		{CodeConfig, 2},
		{CodeToolchain, 3},
		{CodeBuild, 4},
		{CodeVerify, 5},
	} {
		err := New(tc.code, "op", errors.New("boom"))
		if got := ExitCode(err); got != tc.want {
			t.Fatalf("code %d -> %d, want %d", tc.code, got, tc.want)
		}
		if !Is(err) {
			t.Fatalf("Is(%v) = false", err)
		}
		if err.Error() != "op: boom" {
			t.Fatalf("message = %q", err.Error())
		}
	}
	if New(CodeBuild, "", nil) != nil {
		t.Fatal("New with nil error must return nil")
	}
}

func TestExitCodeWrapped(t *testing.T) {
	base := New(CodeVerify, "", errors.New("bad"))
	wrapped := fmt.Errorf("outer: %w", base)
	if got := ExitCode(wrapped); got != 5 {
		t.Fatalf("wrapped exit = %d, want 5", got)
	}
	if !Is(wrapped) {
		t.Fatal("Is should see through fmt wrap")
	}
}
