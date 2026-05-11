package main

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
)

// ── Library item types ────────────────────────────────────────────────────────

type LibItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type LibVideo struct {
	ID      string      `json:"id"`
	Title   string      `json:"title"`
	Streams []LibStream `json:"streams"`
}

type LibStream struct {
	URL string `json:"url"`
}

type LibSource struct {
	AddonName   string
	CatalogID   string
	CatalogName string
	CatalogType string
	Base        string
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func addonBase(a Addon) string {
	return strings.TrimSuffix(strings.TrimRight(a.TransportURL, "/"), "/manifest.json")
}

var libraryKeywords = []string{"dmm", "debrid media manager", "torrentio"}

func isLibraryAddon(name string) bool {
	lower := strings.ToLower(name)
	for _, kw := range libraryKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func collectLibSources(addons []Addon) []LibSource {
	var sources []LibSource
	for _, a := range addons {
		if !isLibraryAddon(a.Manifest.Name) {
			continue
		}
		base := addonBase(a)
		if base == "" {
			continue
		}
		for _, cat := range a.Manifest.Catalogs {
			if cat.Type != "other" {
				continue
			}
			sources = append(sources, LibSource{
				AddonName:   a.Manifest.Name,
				CatalogID:   cat.ID,
				CatalogName: cat.Name,
				CatalogType: cat.Type,
				Base:        base,
			})
		}
	}
	return sources
}

// ── API calls ─────────────────────────────────────────────────────────────────

func fetchLibPage(src LibSource, skip int) ([]LibItem, bool, error) {
	var u string
	if src.CatalogType == "other" {
		u = fmt.Sprintf("%s/catalog/%s/%s/skip=%d.json", src.Base, src.CatalogType, src.CatalogID, skip)
	} else {
		u = fmt.Sprintf("%s/catalog/%s/%s.json", src.Base, src.CatalogType, src.CatalogID)
	}
	var resp struct {
		Metas   []LibItem `json:"metas"`
		HasMore bool      `json:"hasMore"`
	}
	if err := getJSON(u, &resp); err != nil {
		return nil, false, err
	}
	needsEnrich := false
	for i := range resp.Metas {
		if resp.Metas[i].Name == "" && resp.Metas[i].ID != "" {
			needsEnrich = true
			break
		}
	}
	if needsEnrich {
		var wg sync.WaitGroup
		for i := range resp.Metas {
			if resp.Metas[i].Name == "" && resp.Metas[i].ID != "" {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					resp.Metas[i].Name = enrichName(resp.Metas[i].ID, resp.Metas[i].Type)
				}(i)
			}
		}
		wg.Wait()
	}
	return resp.Metas, resp.HasMore, nil
}

func enrichName(id, mediaType string) string {
	cinemetaType := "movie"
	if mediaType == "series" {
		cinemetaType = "series"
	}
	var detail struct {
		Meta struct {
			Name string `json:"name"`
		} `json:"meta"`
	}
	u := fmt.Sprintf("%s/meta/%s/%s.json", urlCinemeta, cinemetaType, id)
	if getJSON(u, &detail) == nil && detail.Meta.Name != "" {
		return detail.Meta.Name
	}
	return id
}

func fetchLibMeta(src LibSource, itemID string) ([]LibVideo, string, error) {
	u := fmt.Sprintf("%s/meta/%s/%s.json", src.Base, src.CatalogType, url.PathEscape(itemID))
	var resp struct {
		Meta struct {
			Name   string     `json:"name"`
			Videos []LibVideo `json:"videos"`
		} `json:"meta"`
	}
	if err := getJSON(u, &resp); err != nil {
		return nil, "", err
	}
	return resp.Meta.Videos, resp.Meta.Name, nil
}

// ── File picker ───────────────────────────────────────────────────────────────

func folderOf(title string) string {
	stripped := strings.TrimPrefix(title, "/")
	idx := strings.Index(stripped, "/")
	if idx < 0 {
		return ""
	}
	folder := stripped[:idx]
	if folder == "" {
		return ""
	}
	return folder
}

func fileOf(title string) string {
	stripped := strings.TrimPrefix(title, "/")
	idx := strings.Index(stripped, "/")
	if idx < 0 {
		return stripped
	}
	return stripped[idx+1:]
}

func playVideo(cfg AppConfig, v LibVideo) {
	if len(v.Streams) == 0 || v.Streams[0].URL == "" {
		fail("no stream URL for this file")
		hint("press enter to continue")
		readLine()
		return
	}
	header("playing")
	fmt.Printf("  %s\n\n", grey(fileOf(v.Title)))
	if err := PlayStream(cfg, v.Streams[0].URL); err != nil {
		fail(err.Error())
		hint("press enter to go back")
		readLine()
	}
}

func libFolderScreen(cfg AppConfig, torrentName, folder string, videos []LibVideo) navAct {
	var folderVideos []LibVideo
	for _, v := range videos {
		if folderOf(v.Title) == folder {
			folderVideos = append(folderVideos, v)
		}
	}

	origVideos := make([]LibVideo, len(folderVideos))
	copy(origVideos, folderVideos)
	sortModes := []string{"original", "A→Z", "Z→A"}
	sortIdx := 0

	applySort := func() {
		switch sortIdx {
		case 1:
			sort.Slice(folderVideos, func(i, j int) bool {
				return strings.ToLower(fileOf(folderVideos[i].Title)) < strings.ToLower(fileOf(folderVideos[j].Title))
			})
		case 2:
			sort.Slice(folderVideos, func(i, j int) bool {
				return strings.ToLower(fileOf(folderVideos[i].Title)) > strings.ToLower(fileOf(folderVideos[j].Title))
			})
		default:
			copy(folderVideos, origVideos)
		}
	}

	page := 0
	for {
		items := make([]Item, len(folderVideos))
		for i, v := range folderVideos {
			items[i] = Item{Label: bold(fileOf(v.Title))}
		}
		res := ShowList(ListOpts{
			Title:   torrentName + " > " + folder,
			Sub:     "r=sort: " + sortModes[sortIdx],
			Items:   items,
			Page:    page,
			CanBack: true,
		})
		switch res.Act {
		case ActQuit:
			return navQuit
		case ActBack:
			return navBack
		case ActNextPage:
			page++
		case ActPrevPage:
			page--
		case ActPick:
			if res.Raw == "r" {
				sortIdx = (sortIdx + 1) % len(sortModes)
				applySort()
				page = 0
				continue
			}
			if res.Pick < 0 || res.Pick >= len(folderVideos) {
				continue
			}
			playVideo(cfg, folderVideos[res.Pick])
		}
	}
}

func libFileScreen(cfg AppConfig, src LibSource, item LibItem) navAct {
	header("library > " + item.Name)
	blank()
	spin("fetching files...")

	videos, torrentName, err := fetchLibMeta(src, item.ID)
	if err != nil || len(videos) == 0 {
		blank()
		fail("couldn't load files")
		hint("press enter to go back")
		readLine()
		return navBack
	}

	folders := []string{}
	folderSeen := map[string]bool{}
	var rootVideos []LibVideo
	for _, v := range videos {
		f := folderOf(v.Title)
		if f == "" {
			rootVideos = append(rootVideos, v)
		} else if !folderSeen[f] {
			folderSeen[f] = true
			folders = append(folders, f)
		}
	}

	if len(folders) > 0 {
		type entry struct {
			isFolder bool
			folder   string
			video    LibVideo
		}
		var entries []entry
		for _, f := range folders {
			entries = append(entries, entry{isFolder: true, folder: f})
		}
		for _, v := range rootVideos {
			entries = append(entries, entry{video: v})
		}

		page := 0
		for {
			items := make([]Item, len(entries))
			for i, e := range entries {
				if e.isFolder {
					items[i] = Item{Label: bold(e.folder), Badge: grey("[folder]")}
				} else {
					items[i] = Item{Label: bold(fileOf(e.video.Title))}
				}
			}
			res := ShowList(ListOpts{
				Title:   torrentName,
				Items:   items,
				Page:    page,
				CanBack: true,
			})
			switch res.Act {
			case ActQuit:
				return navQuit
			case ActBack:
				return navBack
			case ActNextPage:
				page++
			case ActPrevPage:
				page--
			case ActPick:
				if res.Pick < 0 || res.Pick >= len(entries) {
					continue
				}
				e := entries[res.Pick]
				if e.isFolder {
					act := libFolderScreen(cfg, torrentName, e.folder, videos)
					if act == navQuit {
						return navQuit
					}
				} else {
					playVideo(cfg, e.video)
				}
			}
		}
	}

	// No folders — flat list with sort
	origVideos := make([]LibVideo, len(videos))
	copy(origVideos, videos)
	sortModes := []string{"original", "A→Z", "Z→A"}
	sortIdx := 0

	applySort := func() {
		switch sortIdx {
		case 1:
			sort.Slice(videos, func(i, j int) bool {
				return strings.ToLower(videos[i].Title) < strings.ToLower(videos[j].Title)
			})
		case 2:
			sort.Slice(videos, func(i, j int) bool {
				return strings.ToLower(videos[i].Title) > strings.ToLower(videos[j].Title)
			})
		default:
			copy(videos, origVideos)
		}
	}

	page := 0
	for {
		items := make([]Item, len(videos))
		for i, v := range videos {
			title := v.Title
			if title == "" {
				title = fmt.Sprintf("file %d", i+1)
			}
			items[i] = Item{Label: bold(title)}
		}
		res := ShowList(ListOpts{
			Title:   torrentName,
			Sub:     "r=sort: " + sortModes[sortIdx],
			Items:   items,
			Page:    page,
			CanBack: true,
		})
		switch res.Act {
		case ActQuit:
			return navQuit
		case ActBack:
			return navBack
		case ActNextPage:
			page++
		case ActPrevPage:
			page--
		case ActPick:
			if res.Raw == "r" {
				sortIdx = (sortIdx + 1) % len(sortModes)
				applySort()
				page = 0
				continue
			}
			if res.Pick < 0 || res.Pick >= len(videos) {
				continue
			}
			playVideo(cfg, videos[res.Pick])
		}
	}
}

// ── Catalog browser ───────────────────────────────────────────────────────────

// libCatalogScreen shows a paginated list of library items.
// For "other" catalogs (DMM), items come in fixed batches of 12 from the API
// regardless of screen size. We track loaded vs total separately so pagination
// always reflects what's actually on screen, not what the API returned.
func libCatalogScreen(addons []Addon, cfg AppConfig, src LibSource) navAct {
	var items []LibItem
	hasMore := true
	uiPage := 0

	fetchMore := func() {
		if !hasMore {
			return
		}
		newItems, more, err := fetchLibPage(src, len(items))
		if err == nil {
			items = append(items, newItems...)
		}
		hasMore = more
		if src.CatalogType != "other" {
			hasMore = false
		}
	}

	// Initial load — fetch until we have enough to show at least one screen.
	header("library")
	blank()
	spin("loading...")
	for len(items) == 0 || (hasMore && len(items) < contentRows(true)) {
		fetchMore()
		if len(items) == 0 && !hasMore {
			break
		}
	}

	if len(items) == 0 {
		blank()
		fail("nothing found in this catalog")
		hint("press enter to go back")
		readLine()
		return navBack
	}

	for {
		pageSize := contentRows(true)
		neededForPage := (uiPage + 1) * pageSize

		// Fetch until we have enough to fill the current page.
		for len(items) < neededForPage && hasMore {
			fetchMore()
		}

		listItems := make([]Item, len(items))
		for i, it := range items {
			name := it.Name
			if name == "" {
				name = it.ID
			}
			badge := ""
			switch it.Type {
			case "movie":
				badge = blue("[movie]")
			case "series":
				badge = hi("[show]")
			}
			listItems[i] = Item{Label: bold(name), Badge: badge}
		}

		// Show n=load more when the next page would start beyond what we have,
		// OR when we have exactly a full page loaded (meaning DMM likely has more
		// even if hasMore flipped — we check hasMore to be sure).
		sub := ""
		if hasMore {
			sub = "n=load more"
		}

		res := ShowList(ListOpts{
			Title:   src.AddonName + " > " + src.CatalogName,
			Sub:     sub,
			Items:   listItems,
			Page:    uiPage,
			CanBack: true,
		})

		switch res.Act {
		case ActQuit:
			return navQuit
		case ActBack:
			return navBack
		case ActPrevPage:
			if uiPage > 0 {
				uiPage--
			}
		case ActNextPage:
			uiPage++
		case ActPick:
			if res.Raw == "n" && hasMore {
				prevLen := len(items)
				header("library")
				blank()
				spin("loading more...")
				target := prevLen + pageSize
				for len(items) < target && hasMore {
					fetchMore()
				}
				uiPage = prevLen / pageSize
			} else if res.Pick >= 0 && res.Pick < len(items) {
				act := libFileScreen(cfg, src, items[res.Pick])
				if act == navQuit {
					return navQuit
				}
			}
		}
	}
}

// ── Source picker ─────────────────────────────────────────────────────────────

func libraryScreen(addons []Addon, cfg AppConfig) {
	sources := collectLibSources(addons)

	if len(sources) == 0 {
		header("library")
		blank()
		fail("no library addons found")
		hint("install DMM or Torrentio with RD in Stremio")
		blank()
		hint("press enter to go back")
		readLine()
		return
	}

	if len(sources) == 1 {
		libCatalogScreen(addons, cfg, sources[0])
		return
	}

	page := 0
	for {
		listItems := make([]Item, len(sources))
		for i, s := range sources {
			listItems[i] = Item{
				Label: bold(s.AddonName),
				Sub:   s.CatalogName,
				Badge: grey("[" + s.CatalogType + "]"),
			}
		}

		res := ShowList(ListOpts{
			Title:   "library",
			Sub:     "choose a source",
			Items:   listItems,
			Page:    page,
			CanBack: true,
		})

		switch res.Act {
		case ActQuit:
			return
		case ActBack:
			return
		case ActNextPage:
			page++
		case ActPrevPage:
			page--
		case ActPick:
			if res.Pick < 0 || res.Pick >= len(sources) {
				continue
			}
			act := libCatalogScreen(addons, cfg, sources[res.Pick])
			if act == navQuit {
				return
			}
		}
	}
}
