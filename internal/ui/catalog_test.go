package ui

import (
	"errors"
	"strings"
	"testing"

	"stremio-cliuwu/internal/store"
	"stremio-cliuwu/internal/stremio"
)

func TestFailedLoadMoreCanBeRetried(t *testing.T) {
	useCtx(t, &appCtx{cfg: &store.Config{}})
	s := newCatalogScreen(stremio.CatalogRef{Name: "Top"})
	s.Update(catalogPageMsg{id: s.id, metas: []stremio.Meta{{ID: "tt1", Name: "One"}}, hasMore: true})
	s.loadPage(1)
	s.Update(catalogPageMsg{id: s.id, err: errors.New("timeout")})
	if s.loadingMore {
		t.Fatal("a failed page left loadingMore set")
	}
	for _, it := range s.list.items {
		if strings.Contains(it.Label, "loading…") {
			t.Fatal("row still says loading…")
		}
	}
}

func TestGenrePickerLeftWithoutChoosing(t *testing.T) {
	useCtx(t, &appCtx{cfg: &store.Config{}})
	s := newCatalogScreen(stremio.CatalogRef{Name: "Top", NeedsGenre: true, Genres: []string{"Drama"}})
	s.Init()
	if !strings.Contains(s.View(), "needs a genre") {
		t.Fatalf("view: %q", s.View())
	}
}

// useCtx swaps in a test context and puts the old one back afterwards.
func useCtx(t *testing.T, c *appCtx) {
	saved := ctx
	t.Cleanup(func() { ctx = saved })
	ctx = c
}

// Addons edited while the launch load is still running: the slower launch
// result mustn't replace the reload's.
func TestLaunchAddonsDontOverwriteReload(t *testing.T) {
	useCtx(t, &appCtx{cfg: &store.Config{}, loading: true})
	a := newApp(&catalogScreen{})
	a.Update(reloadAddonsMsg{})
	newer := []stremio.Addon{{TransportURL: "https://new.example/manifest.json"}}
	a.Update(addonsReloadedMsg{gen: a.reloadGen, addons: newer})
	a.Update(addonsReadyMsg{Addons: []stremio.Addon{{TransportURL: "https://old.example/manifest.json"}}})
	if len(ctx.addons) != 1 || ctx.addons[0].TransportURL != newer[0].TransportURL {
		t.Fatalf("launch load replaced the reload: %v", ctx.addons)
	}
	if ctx.loading {
		t.Fatal("still marked loading")
	}
}
