package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCorruptFileIsMovedAside(t *testing.T) {
	dir := t.TempDir()
	for _, v := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "APPDATA"} {
		t.Setenv(v, dir)
	}
	var told []string
	OnQuarantine(func(name string) { told = append(told, name) })
	defer OnQuarantine(nil)

	path := favsFile()
	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, []byte(`{"items": [ oops`), 0644)

	if fl := LoadFavs(); len(fl.Items) != 0 {
		t.Fatal("expected an empty list")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("corrupt file left in place for the next save to overwrite")
	}
	if len(told) != 1 || !strings.HasPrefix(told[0], filepath.Base(path)+".corrupt-") {
		t.Fatalf("told %v", told)
	}
	moved, _ := os.ReadFile(filepath.Join(filepath.Dir(path), told[0]))
	if string(moved) != `{"items": [ oops` {
		t.Fatal("moved copy lost its contents")
	}
	// A missing file is not corrupt.
	LoadFavs()
	if len(told) != 1 {
		t.Fatal("missing file reported as corrupt")
	}
}

// A field of the wrong type isn't corruption: the rest of the file loads.
func TestTypeMismatchKeepsTheRest(t *testing.T) {
	dir := t.TempDir()
	for _, v := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "APPDATA"} {
		t.Setenv(v, dir)
	}
	path := CfgFile()
	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, []byte(`{"mpv_path": "/opt/mpv", "posters": "yes"}`), 0644)

	c := LoadConfig()
	if c.MpvPath != "/opt/mpv" {
		t.Fatalf("mpv_path lost: %q", c.MpvPath)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("file moved aside over one mistyped field")
	}
}
