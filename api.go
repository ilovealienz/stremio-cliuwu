package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	urlCinemeta = "https://v3-cinemeta.strem.io"
	urlKitsu    = "https://anime-kitsu.strem.fun"
	urlStremio  = "https://api.strem.io/api"
	urlOMDB     = "https://www.omdbapi.com"
)

var httpClient = &http.Client{
	Timeout: 15 * time.Second,
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:       10 * time.Second,
			KeepAlive:     30 * time.Second,
			FallbackDelay: -1, // prefer IPv4
		}).DialContext,
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: 10 * time.Second,
	},
}

func getJSON(u string, out any) error {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "en-GB,en;q=0.9")
	r, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode >= 400 {
		return fmt.Errorf("http %d", r.StatusCode)
	}
	return json.NewDecoder(r.Body).Decode(out)
}

func postJSON(u string, body, out any) error {
	b, _ := json.Marshal(body)
	r, err := httpClient.Post(u, "application/json", strings.NewReader(string(b)))
	if err != nil {
		return err
	}
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(out)
}

// ── Cache ─────────────────────────────────────────────────────────────────────

var (
	cacheBrowse = map[string][]Meta{}
	cacheSearch = map[string][]Meta{}
	cacheSeries = map[string]SeriesMeta{}
	cacheEpData = map[string]map[int]map[int]Video{} // id → season → ep → Video
	cacheStreams = map[string][]Stream{}
)

// ── Year enrichment ───────────────────────────────────────────────────────────

func enrichYear(m *Meta, omdbKey string) {
	if m.Year != "" {
		return
	}
	var base, mt string
	switch m.Source {
	case "anime":
		base, mt = urlKitsu, "series"
	case "show":
		base, mt = urlCinemeta, "series"
	default:
		base, mt = urlCinemeta, "movie"
	}
	var detail struct {
		Meta struct {
			Year        string `json:"year"`
			ReleaseInfo string `json:"releaseInfo"`
		} `json:"meta"`
	}
	if getJSON(fmt.Sprintf("%s/meta/%s/%s.json", base, mt, url.PathEscape(m.ID)), &detail) == nil {
		if detail.Meta.Year != "" {
			m.Year = detail.Meta.Year
		} else if detail.Meta.ReleaseInfo != "" {
			m.Year = detail.Meta.ReleaseInfo
		}
	}
	// OMDB fallback for IMDB IDs
	if m.Year == "" && strings.HasPrefix(m.ID, "tt") && omdbKey != "" {
		var omdb struct{ Year string `json:"Year"` }
		if getJSON(fmt.Sprintf("%s/?i=%s&apikey=%s", urlOMDB, m.ID, omdbKey), &omdb) == nil {
			m.Year = omdb.Year
		}
	}
}

// ── Account ───────────────────────────────────────────────────────────────────

func StremioLogin() (AuthData, error) {
	blank()
	fmt.Printf("  %s  email + password\n", bold("[1]"))
	fmt.Printf("  %s  paste authKey\n", bold("[2]"))
	blank()
	hint("get authKey → web.stremio.com › F12 console:")
	hint(`JSON.parse(localStorage.getItem("profile")).auth.key`)
	blank()

	if prompt("choice") == "2" {
		return AuthData{AuthKey: prompt("authKey")}, nil
	}
	email := prompt("email")
	pw := readPassword("password")
	spin("logging in...")

	var res struct {
		Result *struct {
			AuthKey string `json:"authKey"`
		} `json:"result"`
		Error *struct{ Message string `json:"message"` } `json:"error"`
	}
	if err := postJSON(urlStremio+"/login", map[string]any{
		"type": "Login", "email": email, "password": pw, "facebook": false,
	}, &res); err != nil {
		return AuthData{}, err
	}
	if res.Result == nil || res.Result.AuthKey == "" {
		msg := "unknown error"
		if res.Error != nil {
			msg = res.Error.Message
		}
		return AuthData{}, fmt.Errorf(msg)
	}
	return AuthData{AuthKey: res.Result.AuthKey, Email: email}, nil
}

func GetAddons(key string) ([]Addon, error) {
	var res struct {
		Result *struct {
			Addons []Addon `json:"addons"`
		} `json:"result"`
	}
	err := postJSON(urlStremio+"/addonCollectionGet", map[string]any{
		"type": "AddonCollectionGet", "authKey": key, "update": true,
	}, &res)
	if err != nil || res.Result == nil {
		return nil, fmt.Errorf("failed to load addons")
	}
	return res.Result.Addons, nil
}

// ── Browse ────────────────────────────────────────────────────────────────────

func Browse(source string, page, perPage int) ([]Meta, bool) {
	key := source + ":all"
	if _, ok := cacheBrowse[key]; !ok {
		var all []Meta
		switch source {
		case "movie":
			var resp struct{ Metas []Meta `json:"metas"` }
			if getJSON(urlCinemeta+"/catalog/movie/top.json", &resp) == nil {
				for i := range resp.Metas {
					resp.Metas[i].Source = "movie"
					resp.Metas[i].Type = "movie"
					enrichYear(&resp.Metas[i], "")
				}
				all = resp.Metas
			}
		case "show":
			var resp struct{ Metas []Meta `json:"metas"` }
			if getJSON(urlCinemeta+"/catalog/series/top.json", &resp) == nil {
				for i := range resp.Metas {
					resp.Metas[i].Source = "show"
					resp.Metas[i].Type = "series"
					enrichYear(&resp.Metas[i], "")
				}
				all = resp.Metas
			}
		case "anime":
			var resp struct{ Metas []Meta `json:"metas"` }
			for _, id := range []string{
				"kitsu-anime-popular", "kitsu-anime-airing",
				"kitsu-anime-rating", "kitsu-anime-trending",
			} {
				if getJSON(fmt.Sprintf("%s/catalog/anime/%s.json", urlKitsu, id), &resp) == nil && len(resp.Metas) > 0 {
					break
				}
			}
			for i := range resp.Metas {
				resp.Metas[i].Source = "anime"
				resp.Metas[i].Type = "series"
			}
			all = resp.Metas
		}
		cacheBrowse[key] = all
	}

	all := cacheBrowse[key]
	start := page * perPage
	if start >= len(all) {
		return nil, false
	}
	end := start + perPage
	hasMore := end < len(all)
	if end > len(all) {
		end = len(all)
	}
	result := make([]Meta, end-start)
	copy(result, all[start:end])
	return result, hasMore
}

// ── Search ────────────────────────────────────────────────────────────────────

func Search(query, omdbKey string) []Meta {
	key := strings.ToLower(strings.TrimSpace(query))
	if v, ok := cacheSearch[key]; ok {
		return v
	}
	q := url.QueryEscape(query)
	var results []Meta

	var mv struct{ Metas []Meta `json:"metas"` }
	if getJSON(fmt.Sprintf("%s/catalog/movie/top/search=%s.json", urlCinemeta, q), &mv) == nil {
		for i := range mv.Metas {
			mv.Metas[i].Source = "movie"
			mv.Metas[i].Type = "movie"
			enrichYear(&mv.Metas[i], omdbKey)
		}
		results = append(results, mv.Metas...)
	}

	var sv struct{ Metas []Meta `json:"metas"` }
	if getJSON(fmt.Sprintf("%s/catalog/series/top/search=%s.json", urlCinemeta, q), &sv) == nil {
		for i := range sv.Metas {
			sv.Metas[i].Source = "show"
			sv.Metas[i].Type = "series"
			enrichYear(&sv.Metas[i], omdbKey)
		}
		results = append(results, sv.Metas...)
	}

	var av struct{ Metas []Meta `json:"metas"` }
	if getJSON(fmt.Sprintf("%s/catalog/anime/kitsu-anime-list/search=%s.json", urlKitsu, q), &av) == nil {
		for i := range av.Metas {
			av.Metas[i].Source = "anime"
			av.Metas[i].Type = "series"
		}
		results = append(results, av.Metas...)
	}

	cacheSearch[key] = results
	return results
}

// ── Series meta ───────────────────────────────────────────────────────────────

func GetSeriesMeta(m Meta) SeriesMeta {
	if v, ok := cacheSeries[m.ID]; ok {
		return v
	}
	var resp struct{ Meta SeriesMeta `json:"meta"` }
	base := urlCinemeta
	if m.Source == "anime" {
		base = urlKitsu
	}
	getJSON(fmt.Sprintf("%s/meta/series/%s.json", base, url.PathEscape(m.ID)), &resp)
	cacheSeries[m.ID] = resp.Meta
	return resp.Meta
}

// GetSeasonEpisodes returns sorted episodes for a season, enriched with OMDB data.
func GetSeasonEpisodes(m Meta, season int, sm SeriesMeta, omdbKey string) []Video {
	// Check cache
	if cacheEpData[m.ID] == nil {
		cacheEpData[m.ID] = map[int]map[int]Video{}
	}
	if _, ok := cacheEpData[m.ID][season]; ok {
		eps := make([]Video, 0, len(cacheEpData[m.ID][season]))
		for _, v := range cacheEpData[m.ID][season] {
			eps = append(eps, v)
		}
		sort.Slice(eps, func(i, j int) bool { return eps[i].Episode < eps[j].Episode })
		return eps
	}

	// Build from sm.Videos
	byEp := map[int]Video{}
	for _, v := range sm.Videos {
		if v.Season == season {
			byEp[v.Episode] = v
		}
	}

	// Enrich from OMDB for IMDB-based shows
	if m.Source != "anime" && strings.HasPrefix(m.ID, "tt") && omdbKey != "" {
		var resp struct {
			Episodes []struct {
				Episode  string `json:"Episode"`
				Title    string `json:"Title"`
				Released string `json:"Released"`
			} `json:"Episodes"`
		}
		if getJSON(fmt.Sprintf("%s/?i=%s&Season=%d&apikey=%s", urlOMDB, m.ID, season, omdbKey), &resp) == nil {
			for _, e := range resp.Episodes {
				n := 0
				fmt.Sscanf(e.Episode, "%d", &n)
				if n > 0 {
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
	}

	cacheEpData[m.ID][season] = byEp

	eps := make([]Video, 0, len(byEp))
	for _, v := range byEp {
		eps = append(eps, v)
	}
	sort.Slice(eps, func(i, j int) bool { return eps[i].Episode < eps[j].Episode })
	return eps
}

// ── Streams ───────────────────────────────────────────────────────────────────

func GetStreams(addons []Addon, mediaType, videoID string) []Stream {
	if v, ok := cacheStreams[videoID]; ok {
		return v
	}
	var all []Stream
	for _, a := range addons {
		if !a.SupportsStream(mediaType, videoID) {
			continue
		}
		name := a.Manifest.Name
		base := strings.TrimSuffix(strings.TrimRight(a.TransportURL, "/"), "/manifest.json")
		if base == "" {
			continue
		}
		// Warn and skip addons that are only reachable on localhost — the CLI
		// runs outside the Stremio app process so it cannot reach them.
		if strings.HasPrefix(base, "http://127.") || strings.HasPrefix(base, "http://localhost") {
			fmt.Printf("  %s  %s  %s\n", grey("⚠"), grey(name), grey("(localhost addon — skipped)"))
			continue
		}
		u := fmt.Sprintf("%s/stream/%s/%s.json", base, mediaType, url.PathEscape(videoID))
		var resp struct{ Streams []Stream `json:"streams"` }
		if getJSON(u, &resp) == nil && len(resp.Streams) > 0 {
			fmt.Printf("  %s  %s  %s\n", good("✓"), bold(name), grey(fmt.Sprintf("(%d)", len(resp.Streams))))
			for i := range resp.Streams {
				resp.Streams[i].Addon = name
			}
			all = append(all, resp.Streams...)
		} else {
			fmt.Printf("  %s  %s\n", grey("–"), grey(name))
		}
	}
	cacheStreams[videoID] = all
	return all
}
