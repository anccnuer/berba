package cli

import (
	"reflect"
	"testing"
)

func TestParseArgsInterspersed(t *testing.T) {
	fs := newFlagSet("test", "test")
	home := fs.String("home", "", "")
	debug := fs.Bool("debug", false, "")

	args := []string{"proj", "--home", "dist", "--debug", "extra"}
	if err := parseArgs(fs, args); err != nil {
		t.Fatal(err)
	}
	if *home != "dist" {
		t.Fatalf("home = %q", *home)
	}
	if !*debug {
		t.Fatal("debug should be true")
	}
	if got, want := fs.Args(), []string{"proj", "extra"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("positionals = %v, want %v", got, want)
	}
}

func TestParseArgsEqualsForm(t *testing.T) {
	fs := newFlagSet("test", "test")
	home := fs.String("home", "", "")
	if err := parseArgs(fs, []string{"--home=dist", "proj"}); err != nil {
		t.Fatal(err)
	}
	if *home != "dist" {
		t.Fatalf("home = %q", *home)
	}
	if got, want := fs.Args(), []string{"proj"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("positionals = %v, want %v", got, want)
	}
}

func TestParseArgsDoubleDash(t *testing.T) {
	fs := newFlagSet("test", "test")
	debug := fs.Bool("debug", false, "")
	if err := parseArgs(fs, []string{"proj", "--", "--debug"}); err != nil {
		t.Fatal(err)
	}
	if *debug {
		t.Fatal("flag after -- must stay positional")
	}
	if got, want := fs.Args(), []string{"proj", "--debug"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("positionals = %v, want %v", got, want)
	}
}
