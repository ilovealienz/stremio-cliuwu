package stremio

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// world answers addon requests by host and path, and counts them.
type world struct {
	mu     sync.Mutex
	routes map[string]any // "host/path" -> JSON body; missing is a 404
	hits   map[string]int
}

func (w *world) RoundTrip(r *http.Request) (*http.Response, error) {
	key := r.URL.Host + r.URL.EscapedPath()
	if r.URL.RawQuery != "" {
		key += "?" + r.URL.RawQuery
	}
	w.mu.Lock()
	w.hits[key]++
	body, ok := w.routes[key]
	w.mu.Unlock()
	rec := httptest.NewRecorder()
	if !ok {
		rec.WriteHeader(http.StatusNotFound)
		return rec.Result(), nil
	}
	json.NewEncoder(rec).Encode(body)
	return rec.Result(), nil
}

func (w *world) count(key string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.hits[key]
}

func useWorld(t *testing.T, routes map[string]any) *world {
	w := &world{routes: routes, hits: map[string]int{}}
	saved := HTTPClient.Transport
	HTTPClient.Transport = w
	clearCaches()
	t.Cleanup(func() {
		HTTPClient.Transport = saved
		clearCaches()
	})
	return w
}

// clearCaches empties every response cache, so no test sees another's
// results — including an earlier run of itself under -count.
func clearCaches() {
	cacheCatalog.Clear()
	cacheSearch.Clear()
	cacheSeries.Clear()
	CacheMetaDetail.Clear()
	CacheSubs.Clear()
	ClearStreams()
}

func manifestAddon(host string, m string) Addon {
	var man Manifest
	if err := json.Unmarshal([]byte(m), &man); err != nil {
		panic(err)
	}
	return Addon{TransportURL: "https://" + host + "/manifest.json", Manifest: man}
}

// Manifests declare resources as bare names or as objects with types and id
// prefixes; both have to route the same way.
func TestResourceRouting(t *testing.T) {
	cinemeta := manifestAddon("cinemeta.test", `{"name":"Cinemeta","resources":["catalog",{"name":"meta","types":["movie","series"],"idPrefixes":["tt"]}]}`)
	kitsu := manifestAddon("kitsu.test", `{"name":"Kitsu","resources":[{"name":"meta","types":["anime","series"],"idPrefixes":["kitsu:"]},"catalog"]}`)
	streams := manifestAddon("torrentio.test", `{"name":"Torrentio","resources":["stream"],"types":["movie","series"]}`)

	cases := []struct {
		a            Addon
		res, typ, id string
		want         bool
	}{
		{cinemeta, "meta", "movie", "tt0111161", true},
		{cinemeta, "meta", "series", "kitsu:1", false},
		{cinemeta, "meta", "anime", "tt1", false},
		{kitsu, "meta", "series", "kitsu:1", true},
		{kitsu, "meta", "movie", "kitsu:1", false},
		{streams, "stream", "series", "kitsu:1:2", true}, // bare name: no limits
		{streams, "meta", "movie", "tt1", false},
	}
	for _, c := range cases {
		if got := c.a.SupportsResource(c.res, c.typ, c.id); got != c.want {
			t.Errorf("%s %s %s %s = %v, want %v", c.a.Manifest.Name, c.res, c.typ, c.id, got, c.want)
		}
	}
	if !streams.HasStreams() || cinemeta.HasStreams() {
		t.Error("HasStreams wrong")
	}
}

func TestCatalogDiscovery(t *testing.T) {
	a := manifestAddon("cat.test", `{"name":"Cats","catalogs":[
		{"type":"movie","id":"top","name":"Top","extra":[{"name":"search"},{"name":"skip"}]},
		{"type":"series","id":"bygenre","extra":[{"name":"genre","isRequired":true,"options":["Drama","Comedy"]}]},
		{"type":"anime","id":"old","extraSupported":["search"],"extraRequired":["skip"]},
		{"type":"movie","id":"needsid","extra":[{"name":"imdb","isRequired":true}]},
		{"type":"other","id":"lib"}
	]}`)
	broken := manifestAddon("broken.test", `{"name":"Broken","catalogs":[{"type":"movie","id":"x"}]}`)
	broken.Err = http.ErrHandlerTimeout

	refs := Catalogs([]Addon{a, broken})
	var ids []string
	for _, r := range refs {
		ids = append(ids, r.ID)
	}
	if strings.Join(ids, ",") != "top,bygenre,old,lib" {
		t.Fatalf("browsable catalogs = %v", ids)
	}
	if !refs[0].Search || !refs[0].Skip || refs[0].NeedsGenre {
		t.Errorf("top: %+v", refs[0])
	}
	if !refs[1].NeedsGenre || len(refs[1].Genres) != 2 || refs[1].Name != "bygenre" {
		t.Errorf("bygenre: %+v", refs[1])
	}
	if !refs[2].Search {
		t.Error("old-style extraSupported search missed")
	}

	kinds := KindsAvailable([]Addon{a})
	if kinds["movie"] != 1 || kinds["show"] != 1 || kinds["anime"] != 1 || kinds["other"] != 1 {
		t.Errorf("kinds = %v", kinds)
	}
	if got := CatalogsOfKind([]Addon{a}, "show"); len(got) != 1 || got[0].ID != "bygenre" {
		t.Errorf("shows = %v", got)
	}
	var search []string
	for _, r := range SearchCatalogs([]Addon{a, broken}) {
		search = append(search, r.ID)
	}
	if strings.Join(search, ",") != "top,old" {
		t.Errorf("search catalogs = %v", search)
	}
}

func TestCatalogURL(t *testing.T) {
	ref := CatalogRef{Base: "https://a.test", Type: "movie", ID: "top list", Skip: true}
	for _, c := range []struct {
		skip  int
		genre string
		want  string
	}{
		{0, "", "https://a.test/catalog/movie/top%20list.json"},
		{100, "", "https://a.test/catalog/movie/top%20list/skip=100.json"},
		{0, "Sci-Fi & Fantasy", "https://a.test/catalog/movie/top%20list/genre=Sci-Fi+%26+Fantasy.json"},
		{50, "Drama", "https://a.test/catalog/movie/top%20list/genre=Drama&skip=50.json"},
	} {
		if got := catalogURL(ref, c.skip, c.genre); got != c.want {
			t.Errorf("skip=%d genre=%q: %s", c.skip, c.genre, got)
		}
	}
	ref.Skip = false
	if got := catalogURL(ref, 100, ""); got != "https://a.test/catalog/movie/top%20list.json" {
		t.Errorf("skip sent to a catalog without skip: %s", got)
	}
}

func TestFetchCatalog(t *testing.T) {
	w := useWorld(t, map[string]any{
		"a.test/catalog/series/top.json": map[string]any{"metas": []any{
			map[string]any{"id": "tt1", "name": "One", "releaseInfo": "2019–2021"},
			map[string]any{"id": 42, "name": "Bad id"}, // wrong type: dropped
			map[string]any{"id": "tt2", "name": "Two", "type": "series", "releaseInfo": "2020"},
		}, "hasMore": true},
	})
	ref := CatalogRef{Base: "https://a.test", Type: "series", ID: "top", Skip: true}
	metas, more, err := FetchCatalog(ref, 0, "")
	if err != nil || !more || len(metas) != 2 {
		t.Fatalf("metas=%v more=%v err=%v", metas, more, err)
	}
	if metas[0].Year != "2019" || metas[0].Source != "show" || metas[0].Type != "series" || metas[0].Base != "https://a.test" {
		t.Errorf("normalised: %+v", metas[0])
	}
	FetchCatalog(ref, 0, "")
	if n := w.count("a.test/catalog/series/top.json"); n != 1 {
		t.Errorf("second fetch hit the network: %d", n)
	}
	if _, _, err := FetchCatalog(CatalogRef{Base: "https://a.test", Type: "movie", ID: "none"}, 0, ""); err == nil {
		t.Error("404 catalog gave no error")
	}
}

func TestSearchMergesAndCaches(t *testing.T) {
	a := manifestAddon("a.test", `{"name":"A","catalogs":[{"type":"movie","id":"m","extra":[{"name":"search"}]}]}`)
	b := manifestAddon("b.test", `{"name":"B","catalogs":[{"type":"movie","id":"m","extra":[{"name":"search"}]},{"type":"series","id":"s","extra":[{"name":"search"}]}]}`)
	w := useWorld(t, map[string]any{
		"a.test/catalog/movie/m/search=the+thing.json": map[string]any{"metas": []any{
			map[string]any{"id": "tt1", "name": "The Thing (A)"},
		}},
		"b.test/catalog/movie/m/search=the+thing.json": map[string]any{"metas": []any{
			map[string]any{"id": "tt1", "name": "The Thing (B)"}, // same title: A wins
			map[string]any{"id": "tt2", "name": "The Thing 2"},
		}},
		// series search 404s: the rest still merge
	})
	got := Search([]Addon{a, b}, "  the thing ")
	if len(got) != 2 || got[0].Name != "The Thing (A)" || got[1].ID != "tt2" {
		t.Fatalf("merged = %+v", got)
	}
	Search([]Addon{a, b}, "THE THING")
	if n := w.count("a.test/catalog/movie/m/search=the+thing.json"); n != 1 {
		t.Errorf("cached search asked again (%d)", n)
	}
	if Search([]Addon{a, b}, "   ") != nil {
		t.Error("blank query searched")
	}
}

func TestSearchByImdbID(t *testing.T) {
	cm := manifestAddon("cinemeta.test", `{"name":"Cinemeta","resources":[{"name":"meta","types":["movie","series"],"idPrefixes":["tt"]}]}`)
	useWorld(t, map[string]any{
		"cinemeta.test/meta/movie/tt0111161.json": map[string]any{"meta": map[string]any{"id": "tt0111161", "name": "The Shawshank Redemption", "releaseInfo": "1994"}},
	})
	for _, q := range []string{"tt0111161", "https://www.imdb.com/title/tt0111161/"} {
		got := Search([]Addon{cm}, q)
		if len(got) != 1 || got[0].Type != "movie" || got[0].Name != "The Shawshank Redemption" || got[0].Source != "movie" {
			t.Errorf("%s: %+v", q, got)
		}
	}
}

// Cinemeta leaves rating and runtime empty on new releases; a later addon
// that has them fills only those gaps.
func TestMetaDetailFillsGaps(t *testing.T) {
	cm := manifestAddon("cinemeta.test", `{"name":"Cinemeta","resources":[{"name":"meta","types":["movie"],"idPrefixes":["tt"]}]}`)
	mf := manifestAddon("mf.test", `{"name":"MediaFusion","resources":[{"name":"meta","types":["movie"],"idPrefixes":["mf:"]}]}`)
	w := useWorld(t, map[string]any{
		"cinemeta.test/meta/movie/tt9.json": map[string]any{"meta": map[string]any{"id": "tt9", "name": "New Film", "description": "From cinemeta"}},
		"mf.test/meta/movie/tt9.json":       map[string]any{"meta": map[string]any{"id": "tt9", "name": "Other Name", "imdbRating": 7.4, "runtime": "101 min"}},
	})
	d, ok := GetMetaDetail([]Addon{cm, mf}, "movie", "tt9", "")
	if !ok || d.Name != "New Film" || d.Description != "From cinemeta" || d.ImdbRating != "7.4" || d.Runtime != "101 min" {
		t.Fatalf("detail = %+v", d)
	}

	// Complete on the first answer: nobody else is asked.
	w.routes["cinemeta.test/meta/movie/tt10.json"] = map[string]any{"meta": map[string]any{"id": "tt10", "name": "Old Film", "imdbRating": "8", "runtime": "90 min"}}
	GetMetaDetail([]Addon{cm, mf}, "movie", "tt10", "")
	if w.count("mf.test/meta/movie/tt10.json") != 0 {
		t.Error("asked a second addon for a complete meta")
	}

	// Nobody has it: cached as missing, so it isn't asked again.
	if _, ok := GetMetaDetail([]Addon{cm}, "movie", "tt404", ""); ok {
		t.Fatal("found a title nobody has")
	}
	GetMetaDetail([]Addon{cm}, "movie", "tt404", "")
	if n := w.count("cinemeta.test/meta/movie/tt404.json"); n != 1 {
		t.Errorf("missing title asked %d times", n)
	}
}

// The addon a title came from is asked first, even one that doesn't declare
// meta for that id.
func TestMetaHintFirst(t *testing.T) {
	cm := manifestAddon("cinemeta.test", `{"name":"Cinemeta","resources":[{"name":"meta","types":["series"],"idPrefixes":["tt"]}]}`)
	got := metaBases([]Addon{cm}, "series", "tt1", "https://lib.test")
	if len(got) != 2 || got[0] != "https://lib.test" || got[1] != "https://cinemeta.test" {
		t.Fatalf("bases = %v", got)
	}
	if got := metaBases([]Addon{cm}, "series", "tt1", "https://cinemeta.test"); len(got) != 1 {
		t.Fatalf("hint listed twice: %v", got)
	}
}

func TestSeriesMeta(t *testing.T) {
	cm := manifestAddon("cinemeta.test", `{"name":"Cinemeta","resources":[{"name":"meta","types":["series"],"idPrefixes":["tt"]}]}`)
	w := useWorld(t, map[string]any{
		"cinemeta.test/meta/series/tt5.json": map[string]any{"meta": map[string]any{"id": "tt5", "name": "Show", "videos": []any{
			map[string]any{"id": "tt5:1:2", "season": 1, "episode": 2, "name": "Second"},
			map[string]any{"id": "tt5:1:1", "season": 1, "episode": 1, "title": "First", "description": "desc"},
			map[string]any{"id": "tt5:0:1", "season": 0, "episode": 1, "title": "Special"},
		}}},
	})
	sm := GetSeriesMeta([]Addon{cm}, Meta{ID: "tt5"})
	if len(sm.Videos) != 3 || sm.Videos[0].Title != "Second" || sm.Videos[1].Overview != "desc" {
		t.Fatalf("videos = %+v", sm.Videos)
	}
	GetSeriesMeta([]Addon{cm}, Meta{ID: "tt5"})
	if w.count("cinemeta.test/meta/series/tt5.json") != 1 {
		t.Error("series meta not cached")
	}

	eps := GetSeasonEpisodes(Meta{ID: "tt5"}, 1, sm, "")
	if len(eps) != 2 || eps[0].Episode != 1 || eps[1].Episode != 2 {
		t.Fatalf("season 1 = %+v", eps)
	}
	if sp := GetSeasonEpisodes(Meta{ID: "tt5"}, 0, sm, ""); len(sp) != 1 || sp[0].Title != "Special" {
		t.Fatalf("specials = %+v", sp)
	}

	// Empty answers aren't cached: the next look asks again.
	GetSeriesMeta([]Addon{cm}, Meta{ID: "tt6"})
	GetSeriesMeta([]Addon{cm}, Meta{ID: "tt6"})
	if n := w.count("cinemeta.test/meta/series/tt6.json"); n != 2 {
		t.Errorf("missing series asked %d times, want 2", n)
	}
}

// OMDb fills missing episode titles, but an episode it lists that the addon
// doesn't have has no id to play and mustn't appear.
func TestSeasonEpisodesOMDb(t *testing.T) {
	useWorld(t, map[string]any{
		"www.omdbapi.com/?i=tt5&Season=1&apikey=k": map[string]any{"Episodes": []any{
			map[string]any{"Episode": "1", "Title": "Pilot", "Released": "2020-01-01"},
			map[string]any{"Episode": "2", "Title": "N/A", "Released": "N/A"},
			map[string]any{"Episode": "3", "Title": "Not On The Addon"},
		}},
	})
	sm := SeriesMeta{Videos: []Video{
		{ID: "tt5:1:1", Season: 1, Episode: 1},
		{ID: "tt5:1:2", Season: 1, Episode: 2, Title: "Kept"},
	}}
	eps := GetSeasonEpisodes(Meta{ID: "tt5", Source: "show"}, 1, sm, "k")
	if len(eps) != 2 {
		t.Fatalf("episodes = %+v", eps)
	}
	if eps[0].Title != "Pilot" || eps[0].Released != "2020-01-01" || eps[1].Title != "Kept" {
		t.Fatalf("enriched = %+v", eps)
	}
}

// A kitsu cour is regrouped into the full show when the addon knows its imdb
// id and the cour is one of the numbered seasons.
func TestKitsuRegroup(t *testing.T) {
	k := manifestAddon("kitsu.test", `{"name":"Kitsu","resources":[{"name":"meta","types":["series","anime"],"idPrefixes":["kitsu:","tt"]}]}`)
	useWorld(t, map[string]any{
		"kitsu.test/meta/series/kitsu:200.json": map[string]any{"meta": map[string]any{"id": "kitsu:200", "imdb_id": "tt77", "videos": []any{
			map[string]any{"id": "kitsu:200:1", "season": 1, "episode": 1},
		}}},
		"kitsu.test/meta/series/tt77.json": map[string]any{"meta": map[string]any{"id": "tt77", "videos": []any{
			map[string]any{"id": "tt77:1:1", "season": 1, "episode": 1, "kitsu_id": "100", "kitsuEpisode": 1},
			map[string]any{"id": "tt77:2:1", "season": 2, "episode": 1, "kitsu_id": "200", "kitsuEpisode": 1},
			map[string]any{"id": "tt77:0:1", "season": 0, "episode": 1, "kitsu_id": "300", "kitsuEpisode": 1},
		}}},
		"kitsu.test/meta/series/kitsu:300.json": map[string]any{"meta": map[string]any{"id": "kitsu:300", "imdb_id": "tt77", "videos": []any{
			map[string]any{"id": "kitsu:300:1", "season": 1, "episode": 1},
		}}},
	})
	sm := GetSeriesMeta([]Addon{k}, Meta{ID: "kitsu:200"})
	if len(sm.Videos) != 3 {
		t.Fatalf("cour not regrouped: %+v", sm.Videos)
	}
	if v := sm.Videos[1]; v.StreamID() != "kitsu:200:1" {
		t.Errorf("stream id = %s, want the kitsu one", v.StreamID())
	}
	// An OVA filed under season 0 keeps its own episodes.
	if sm := GetSeriesMeta([]Addon{k}, Meta{ID: "kitsu:300"}); len(sm.Videos) != 1 {
		t.Fatalf("special regrouped into the show: %+v", sm.Videos)
	}
}

func TestFlexString(t *testing.T) {
	var d MetaDetail
	if err := json.Unmarshal([]byte(`{"runtime":120,"imdbRating":"7.1"}`), &d); err != nil || d.Runtime != "120" || d.ImdbRating != "7.1" {
		t.Fatalf("%+v %v", d, err)
	}
	if err := json.Unmarshal([]byte(`{"runtime":null,"imdbRating":[1,2]}`), &d); err != nil || d.Runtime != "" || d.ImdbRating != "" {
		t.Fatalf("%+v %v", d, err)
	}
}
