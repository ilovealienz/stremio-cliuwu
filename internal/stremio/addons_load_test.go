package stremio

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestLoadAddons(t *testing.T) {
	useWorld(t, map[string]any{
		"a.test/manifest.json":          map[string]any{"name": "Alpha", "resources": []any{"stream"}},
		"b.test/k=SECRET/manifest.json": map[string]any{"id": "org.b", "resources": []any{"stream"}}, // no name
		"c.test/manifest.json":          "not an object",
		"d.test/manifest.json":          map[string]any{"error": "invalid debrid key"},
	})
	al := AddonList{Items: []AddonRef{
		{URL: "https://a.test/manifest.json"},
		{URL: "https://off.test/manifest.json", Disabled: true},
		{URL: "https://b.test/k=SECRET/manifest.json"},
		{URL: "https://c.test/manifest.json"},
		{URL: "https://missing.test/manifest.json"},
		{URL: "https://d.test/manifest.json"},
	}}
	got := LoadAddons(al)
	if len(got) != 5 {
		t.Fatalf("loaded %d, want 5 (disabled skipped, failures kept)", len(got))
	}
	if got[0].Manifest.Name != "Alpha" || got[1].Manifest.Name != "b.test" {
		t.Errorf("names: %q %q", got[0].Manifest.Name, got[1].Manifest.Name)
	}
	if got[2].Err == nil || got[3].Err == nil || got[4].Err == nil {
		t.Errorf("non-manifests and a 404 should fail: %v %v %v", got[2].Err, got[3].Err, got[4].Err)
	}
	ok := OKAddons(got)
	if len(ok) != 2 || ok[1].TransportURL != al.Items[2].URL {
		t.Fatalf("OKAddons = %v", ok)
	}
	for _, a := range got {
		if a.Err != nil && strings.Contains(a.Err.Error(), "SECRET") {
			t.Errorf("error leaks the addon key: %v", a.Err)
		}
	}
}

func TestCleanErr(t *testing.T) {
	inner := errors.New("connection refused")
	err := &url.Error{Op: "Get", URL: "https://torrentio.test/realdebrid=SECRET/manifest.json", Err: inner}
	got := CleanErr(err).Error()
	if strings.Contains(got, "SECRET") || !strings.Contains(got, "torrentio.test") || !errors.Is(CleanErr(err), inner) {
		t.Fatalf("CleanErr = %q", got)
	}
	// The host isn't repeated when the error already names it.
	dns := &url.Error{Op: "Get", URL: "https://x.test/k/manifest.json", Err: errors.New("lookup x.test: no such host")}
	if got := CleanErr(dns).Error(); got != "lookup x.test: no such host" {
		t.Fatalf("CleanErr = %q", got)
	}
	plain := errors.New("plain")
	if CleanErr(plain) != plain {
		t.Fatal("non-url error changed")
	}
}
