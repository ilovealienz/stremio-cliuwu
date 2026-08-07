package main

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// Everything here used to be hardcoded against Cinemeta and Kitsu, which meant
// installing an addon could not add a catalog or extend search — addons only
// mattered for stream resolution. Now browsing and search are both derived
// from the installed manifests.

var (
	cacheCatalog = newCache[[]Meta](10*time.Minute, 200)
	cacheSearch  = newCache[[]Meta](5*time.Minute, 100)
	cacheSeries  = newCache[SeriesMeta](30*time.Minute, 200)
)

// ── Discovery ─────────────────────────────────────────────────────────────────

// Catalogs returns every browsable catalog across the installed addons, in
// addon order.
func Catalogs(addons []Addon) []CatalogRef {
	var out []CatalogRef
	for _, a := range addons {
		if a.Err != nil {
			continue
		}
		base := addonBase(a)
		if base == "" {
			continue
		}
		name := addonLabel(a)
		for _, c := range a.Manifest.Catalogs {
			if !c.Browsable() {
				continue
			}
			label := c.Name
			if label == "" {
				label = c.ID
			}
			out = append(out, CatalogRef{
				AddonName:  name,
				Base:       base,
				Type:       c.Type,
				ID:         c.ID,
				Name:       label,
				Search:     c.Supports("search"),
				Skip:       c.Supports("skip"),
				Genres:     c.Genres(),
				NeedsGenre: c.Requires("genre"),
			})
		}
	}
	return out
}

// CatalogsOfKind filters to one of the top-level menu buckets.
func CatalogsOfKind(addons []Addon, kind string) []CatalogRef {
	var out []CatalogRef
	for _, c := range Catalogs(addons) {
		if c.Kind() == kind {
			out = append(out, c)
		}
	}
	return out
}

// KindsAvailable reports which buckets actually have catalogs behind them, so
// the menu can grey out or hide the empty ones.
func KindsAvailable(addons []Addon) map[string]int {
	counts := map[string]int{}
	for _, c := range Catalogs(addons) {
		counts[c.Kind()]++
	}
	return counts
}

// SearchCatalogs returns catalogs that declare search support.
func SearchCatalogs(addons []Addon) []CatalogRef {
	var out []CatalogRef
	for _, a := range addons {
		if a.Err != nil {
			continue
		}
		base := addonBase(a)
		if base == "" {
			continue
		}
		name := addonLabel(a)
		for _, c := range a.Manifest.Catalogs {
			if !c.Supports("search") {
				continue
			}
			label := c.Name
			if label == "" {
				label = c.ID
			}
			out = append(out, CatalogRef{
				AddonName: name,
				Base:      base,
				Type:      c.Type,
				ID:        c.ID,
				Name:      label,
				Search:    true,
				Skip:      c.Supports("skip"),
			})
		}
	}
	return out
}

// ── Fetching ──────────────────────────────────────────────────────────────────

type catalogResponse struct {
	Metas   []Meta `json:"metas"`
	HasMore bool   `json:"hasMore"`
}

// sourceOf maps a catalog type onto the tag used for badges and history.
func sourceOf(catalogType string) string {
	switch catalogType {
	case "movie":
		return "movie"
	case "anime":
		return "anime"
	case "series":
		return "show"
	}
	return catalogType
}

// catalogURL builds the request, folding optional extras into the path segment
// the protocol expects: /catalog/{type}/{id}/genre=Action&skip=100.json
func catalogURL(ref CatalogRef, skip int, genre string) string {
	var extras []string
	if genre != "" {
		extras = append(extras, "genre="+url.QueryEscape(genre))
	}
	if skip > 0 && ref.Skip {
		extras = append(extras, fmt.Sprintf("skip=%d", skip))
	}
	if len(extras) == 0 {
		return fmt.Sprintf("%s/catalog/%s/%s.json", ref.Base, ref.Type, url.PathEscape(ref.ID))
	}
	return fmt.Sprintf("%s/catalog/%s/%s/%s.json",
		ref.Base, ref.Type, url.PathEscape(ref.ID), strings.Join(extras, "&"))
}

// FetchCatalog pulls one page. skip is ignored by catalogs that don't declare
// skip support, in which case everything arrives in one response.
func FetchCatalog(ref CatalogRef, skip int, genre string) ([]Meta, bool, error) {
	u := catalogURL(ref, skip, genre)

	key := u
	if v, ok := cacheCatalog.Get(key); ok {
		return v, len(v) > 0 && ref.Skip, nil
	}

	var resp catalogResponse
	if err := getJSON(u, &resp); err != nil {
		return nil, false, err
	}

	src := sourceOf(ref.Type)
	for i := range resp.Metas {
		resp.Metas[i].normalize(src, ref.Base)
	}

	cacheCatalog.Set(key, resp.Metas)
	return resp.Metas, resp.HasMore, nil
}

// Search fans out across every search-capable catalog and merges the results.
// Addon order decides precedence when the same title comes back twice.
func Search(addons []Addon, query string) []Meta {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil
	}

	refs := SearchCatalogs(addons)
	if len(refs) == 0 {
		return nil
	}

	key := strings.ToLower(q)
	if v, ok := cacheSearch.Get(key); ok {
		return v
	}

	results := make([][]Meta, len(refs))
	var wg sync.WaitGroup
	for i, ref := range refs {
		wg.Add(1)
		go func(i int, ref CatalogRef) {
			defer wg.Done()
			u := fmt.Sprintf("%s/catalog/%s/%s/search=%s.json",
				ref.Base, ref.Type, url.PathEscape(ref.ID), url.QueryEscape(q))

			var resp catalogResponse
			if getJSON(u, &resp) != nil {
				return
			}
			src := sourceOf(ref.Type)
			for j := range resp.Metas {
				resp.Metas[j].normalize(src, ref.Base)
			}
			results[i] = resp.Metas
		}(i, ref)
	}
	wg.Wait()

	seen := map[string]bool{}
	var merged []Meta
	for _, batch := range results {
		for _, m := range batch {
			k := m.Type + ":" + m.ID
			if m.ID == "" || seen[k] {
				continue
			}
			seen[k] = true
			merged = append(merged, m)
		}
	}

	cacheSearch.Set(key, merged)
	return merged
}

// ── Meta ──────────────────────────────────────────────────────────────────────

// metaBases returns addon bases able to answer a meta request for this id,
// with the addon that produced the item tried first.
func metaBases(addons []Addon, mediaType, id, hint string) []string {
	var out []string
	if hint != "" {
		out = append(out, hint)
	}
	for _, a := range addons {
		if a.Err != nil || !a.SupportsResource("meta", mediaType, id) {
			continue
		}
		base := addonBase(a)
		if base == "" || base == hint {
			continue
		}
		out = append(out, base)
	}
	return out
}

var cacheMetaDetail = newCache[MetaDetail](30*time.Minute, 300)

// GetMetaDetail fetches the full meta object for one title, asking whichever
// addons declare a meta resource for that id.
func GetMetaDetail(addons []Addon, mediaType, id, hint string) (MetaDetail, bool) {
	key := mediaType + ":" + id
	if v, ok := cacheMetaDetail.Get(key); ok {
		return v, v.ID != ""
	}

	for _, base := range metaBases(addons, mediaType, id, hint) {
		var resp struct {
			Meta MetaDetail `json:"meta"`
		}
		u := fmt.Sprintf("%s/meta/%s/%s.json", base, mediaType, url.PathEscape(id))
		if getJSON(u, &resp) == nil && resp.Meta.Name != "" {
			cacheMetaDetail.Set(key, resp.Meta)
			return resp.Meta, true
		}
	}

	cacheMetaDetail.Set(key, MetaDetail{}) // negative cache, don't re-ask
	return MetaDetail{}, false
}

// GetSeriesMeta fetches the episode list, asking each capable addon in turn
// rather than guessing between Cinemeta and Kitsu based on a source tag.
func GetSeriesMeta(addons []Addon, m Meta) SeriesMeta {
	if v, ok := cacheSeries.Get(m.ID); ok {
		return v
	}

	for _, base := range metaBases(addons, "series", m.ID, m.Base) {
		var resp struct {
			Meta SeriesMeta `json:"meta"`
		}
		u := fmt.Sprintf("%s/meta/series/%s.json", base, url.PathEscape(m.ID))
		if getJSON(u, &resp) == nil && len(resp.Meta.Videos) > 0 {
			cacheSeries.Set(m.ID, resp.Meta)
			return resp.Meta
		}
	}

	empty := SeriesMeta{}
	cacheSeries.Set(m.ID, empty)
	return empty
}

// GetSeasonEpisodes returns sorted episodes for a season, enriched from OMDB
// for IMDB-identified shows (Cinemeta often omits episode titles).
func GetSeasonEpisodes(m Meta, season int, sm SeriesMeta, omdbKey string) []Video {
	byEp := map[int]Video{}
	for _, v := range sm.Videos {
		if v.Season == season {
			byEp[v.Episode] = v
		}
	}

	if m.Source != "anime" && strings.HasPrefix(m.ID, "tt") && omdbKey != "" {
		var resp struct {
			Episodes []struct {
				Episode  string `json:"Episode"`
				Title    string `json:"Title"`
				Released string `json:"Released"`
			} `json:"Episodes"`
		}
		u := fmt.Sprintf("%s/?i=%s&Season=%d&apikey=%s", urlOMDB, m.ID, season, omdbKey)
		if getJSON(u, &resp) == nil {
			for _, e := range resp.Episodes {
				n := 0
				fmt.Sscanf(e.Episode, "%d", &n)
				if n <= 0 {
					continue
				}
				v := byEp[n]
				if e.Title != "" && e.Title != "N/A" {
					v.Title = e.Title
				}
				if e.Released != "" && e.Released != "N/A" {
					v.Released = e.Released
				}
				byEp[n] = v
			}
		}
	}

	eps := make([]Video, 0, len(byEp))
	for _, v := range byEp {
		eps = append(eps, v)
	}
	sort.Slice(eps, func(i, j int) bool { return eps[i].Episode < eps[j].Episode })
	return eps
}
