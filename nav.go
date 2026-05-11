package main

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ── Helpers ───────────────────────────────────────────────────────────────────

func fmtRelease(s string) string {
	if s == "" || s == "N/A" {
		return ""
	}
	for _, layout := range []string{
		"2006-01-02",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
		"02 Jan 2006",
		"Jan 2, 2006",
		time.RFC3339,
	} {
		if t, err := time.Parse(layout, s); err == nil {
			// Reject Unix epoch (Jan 1 1970) — means the date field was unset
			if t.IsZero() || (t.Year() == 1970 && t.Month() == 1 && t.Day() == 1) {
				return ""
			}
			return t.Format("Mon 02/01/06")
		}
	}
	return ""
}

func fmtVideoID(videoID string) string {
	parts := strings.Split(videoID, ":")
	if len(parts) >= 3 {
		sStr, eStr := parts[len(parts)-2], parts[len(parts)-1]
		if parts[0] == "kitsu" && len(parts) == 3 {
			sStr = "1"
			eStr = parts[2]
		}
		s, se := strconv.Atoi(sStr)
		e, ee := strconv.Atoi(eStr)
		if se == nil && ee == nil {
			return fmt.Sprintf("S%02dE%02d", s, e)
		}
	}
	return videoID
}

func metaItem(m Meta) Item {
	yr := m.Year
	if yr == "" {
		yr = "?"
	}
	return Item{
		Label: bold(m.Name),
		Sub:   "(" + yr + ")",
		Badge: sourceTag(m.Source),
	}
}

func epItem(v Video, watched bool) Item {
	code := hi(fmt.Sprintf("E%02d", v.Episode))
	sub := ""
	if v.Title != "" {
		sub = v.Title
	}
	badge := ""
	if r := fmtRelease(v.Released); r != "" {
		badge = grey(r)
	}
	return Item{
		Label:   bold(code),
		Sub:     sub,
		Badge:   badge,
		Watched: watched,
	}
}

func addFavFromMeta(m Meta, season int) {
	AddFav(Favourite{
		Name: m.Name, ID: m.ID, Type: m.Type,
		Source: m.Source, Year: m.Year, Season: season,
	})
	if season > 0 {
		ok(fmt.Sprintf("added %s S%02d to favourites", bold(m.Name), season))
	} else {
		ok(fmt.Sprintf("added %s to favourites", bold(m.Name)))
	}
	time.Sleep(700 * time.Millisecond)
}

// ── Stream screen ─────────────────────────────────────────────────────────────

// streamScreen shows streams and handles playback.
// ctx is optional episode context for [ / ] navigation.
// Returns (navAct) — navBack, navBackAll, or navOK.
func streamScreen(addons []Addon, cfg AppConfig, mediaType, videoID string, m Meta, ctx *EpCtx, seekTo ...float64) navAct {
	label := m.Name
	if mediaType == "series" {
		epStr := fmtVideoID(videoID)
		if ctx != nil && len(ctx.Episodes) > 0 {
			epStr += fmt.Sprintf("  ·  %02d/%02d", ctx.Index+1, len(ctx.Episodes))
		}
		label += "  ·  " + epStr
	}

	// Fetch streams (cached)
	header("fetching streams")
	fmt.Printf("  %s\n\n", grey(label))
	all := GetStreams(addons, mediaType, videoID)

	var playable []Stream
	for _, s := range all {
		if s.URL != "" {
			playable = append(playable, s)
		}
	}

	if len(playable) == 0 {
		blank()
		fail("no playable streams found")
		blank()
		hint("press enter to go back")
		readLine()
		return navBack
	}

	playable = SortStreams(playable, cfg.PreferredQuality)

	allLabels := make([]string, len(playable))
	for i, s := range playable {
		allLabels[i] = FmtStream(s)
	}

	// Episode context for [ / ] keys
	hasPrev := ctx != nil && ctx.Index > 0
	hasNext := ctx != nil && ctx.Index < len(ctx.Episodes)-1

	filter  := ""
	page    := 0
	reverse := false

	for {
		// Apply filter
		var filtered []Stream
		var filtLabels []string
		fq := strings.ToLower(filter)
		for i, s := range playable {
			lbl := allLabels[i]
			if fq == "" ||
				strings.Contains(strings.ToLower(s.Addon), fq) ||
				strings.Contains(strings.ToLower(lbl), fq) {
				filtered = append(filtered, s)
				filtLabels = append(filtLabels, lbl)
			}
		}

		if reverse {
			for i, j := 0, len(filtered)-1; i < j; i, j = i+1, j-1 {
				filtered[i], filtered[j] = filtered[j], filtered[i]
				filtLabels[i], filtLabels[j] = filtLabels[j], filtLabels[i]
			}
		}

		perPage := contentRows(true)
		totalPages := (len(filtered) + perPage - 1) / perPage
		if totalPages < 1 {
			totalPages = 1
		}
		if page >= totalPages {
			page = totalPages - 1
		}

		start := page * perPage
		end   := start + perPage
		if end > len(filtered) {
			end = len(filtered)
		}

		// Draw
		header("streams")
		sub := label
		if filter != "" {
			sub += "  ·  " + hi("/"+filter) + "  " + grey(fmt.Sprintf("(%d)", len(filtered)))
		}
		fmt.Printf("  %s\n\n", grey(sub))

		for i := start; i < end; i++ {
			fmt.Printf("  %s  %s\n", accent(fmt.Sprintf("[%2d]", i+1)), filtLabels[i])
		}

		blank()

		var nav []string
		if filter != "" {
			nav = append(nav, grey("c")+"=clear")
		}
		nav = append(nav, grey("/")+"=filter", grey("r")+"=reverse", grey("R")+"=rescan", grey("x")+"=remove")
		if page > 0 {
			nav = append(nav, grey("p")+"=prev")
		}
		if page < totalPages-1 {
			nav = append(nav, grey("n")+"=next")
		}
		if hasPrev {
			nav = append(nav, grey("[")+"=prev ep")
		}
		if hasNext {
			nav = append(nav, grey("]")+"=next ep")
		}
		nav = append(nav, grey("B")+"=back to list", grey("b")+"=back", grey("0")+"=quit")
		if totalPages > 1 {
			nav = append(nav, grey(fmt.Sprintf("p%d/%d", page+1, totalPages)))
		}

		fmt.Printf("  %s\n", strings.Join(nav, "  "))
		fmt.Printf("  %s: ", grey("›"))
		StatusStart()
		raw := readLine()
		StatusStop()

		switch raw {
		case "0", "q":
			os.Exit(0)
		case "b":
			return navBack
		case "B":
			return navBackAll
		case "c":
			filter = ""
			page = 0
			continue
		case "r":
			reverse = !reverse
			continue
		case "R":
			// Clear stream cache for this videoID and refetch
			delete(cacheStreams, videoID)
			header("fetching streams")
			fmt.Printf("  %s\n\n", grey(label))
			all = GetStreams(addons, mediaType, videoID)
			playable = playable[:0]
			for _, s := range all {
				if s.URL != "" {
					playable = append(playable, s)
				}
			}
			playable = SortStreams(playable, cfg.PreferredQuality)
			allLabels = make([]string, len(playable))
			for i, s := range playable {
				allLabels[i] = FmtStream(s)
			}
			filter = ""
			page   = 0
			continue
		case "x":
			// Remove a single stream from the list without rescanning
			header("streams")
			fmt.Printf("  %s\n\n", grey(sub))
			for i := start; i < end; i++ {
				fmt.Printf("  %s  %s\n", accent(fmt.Sprintf("[%2d]", i+1)), filtLabels[i])
			}
			blank()
			fmt.Printf("  %s  %s: ", grey("remove which"), grey("0=cancel"))
			n, err := strconv.Atoi(readLine())
			if err == nil && n >= 1 && n <= len(filtered) {
				// Remove from playable and allLabels
				target := filtered[n-1]
				newPlayable := playable[:0]
				newLabels := make([]string, 0, len(playable))
				for i, s := range playable {
					if s.URL != target.URL {
						newPlayable = append(newPlayable, s)
						newLabels = append(newLabels, allLabels[i])
					}
				}
				playable = newPlayable
				allLabels = newLabels
				// Update cache too so rescan isn't needed
				cacheStreams[videoID] = newPlayable
				filter = ""
				page = 0
			}
			continue
		case "n":
			if page < totalPages-1 {
				page++
			}
			continue
		case "p":
			if page > 0 {
				page--
			}
			continue
		case "/":
			header("streams")
			fmt.Printf("  %s\n\n", grey(label))
			if filter != "" {
				fmt.Printf("  %s %s\n\n", grey("current:"), hi(filter))
			}
			hint("filter by addon, quality, or filename — empty to clear")
			blank()
			fmt.Printf("  %s: ", grey("/"))
			filter = strings.TrimSpace(readLine())
			page = 0
			continue
		case "[":
			if hasPrev {
				ep := ctx.Episodes[ctx.Index-1]
				AddHistory(HistoryEntry{
					Name: ctx.Show.Name, ID: ctx.Show.ID, Type: ctx.Show.Type,
					Source: ctx.Show.Source, Year: ctx.Show.Year,
					Season: ctx.Season, Episode: ep.Episode,
					VideoID: ep.ID, EpTitle: ep.Title,
				}, cfg.HistoryMax)
				newCtx := &EpCtx{
					Show:     ctx.Show,
					Season:   ctx.Season,
					Episodes: ctx.Episodes,
					Index:    ctx.Index - 1,
				}
				return streamScreen(addons, cfg, "series", ep.ID, m, newCtx)
			}
			continue
		case "]":
			if hasNext {
				ep := ctx.Episodes[ctx.Index+1]
				AddHistory(HistoryEntry{
					Name: ctx.Show.Name, ID: ctx.Show.ID, Type: ctx.Show.Type,
					Source: ctx.Show.Source, Year: ctx.Show.Year,
					Season: ctx.Season, Episode: ep.Episode,
					VideoID: ep.ID, EpTitle: ep.Title,
				}, cfg.HistoryMax)
				newCtx := &EpCtx{
					Show:     ctx.Show,
					Season:   ctx.Season,
					Episodes: ctx.Episodes,
					Index:    ctx.Index + 1,
				}
				return streamScreen(addons, cfg, "series", ep.ID, m, newCtx)
			} else if ctx != nil {
				// Last episode of season
				header("streams")
				fmt.Printf("  %s\n\n", grey(label))
				blank()
				fmt.Printf("  %s\n", yell(fmt.Sprintf("last episode of Season %d", ctx.Season)))
				blank()
				hint("press enter to continue")
				readLine()
			}
			continue
		}

		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > len(filtered) {
			fmt.Printf("\n  %s\n", bad(fmt.Sprintf("pick 1–%d or use /", len(filtered))))
			time.Sleep(600 * time.Millisecond)
			continue
		}

		chosen := filtered[n-1]
		wasPlaying := mpvAlive()

		// Register URL→videoID mapping so the ticker can track position correctly
		RegisterVideo(videoID, chosen.URL)

		if err := PlayStream(cfg, chosen.URL); err != nil {
			blank()
			fail("failed: " + err.Error())
			blank()
			hint("press enter to continue")
			readLine()
			continue
		}

		// Seek to resume position on first play
		if !wasPlaying && len(seekTo) > 0 && seekTo[0] > 0 {
			go func() {
				// Poll until mpv is actually playing before seeking
				for i := 0; i < 30; i++ {
					time.Sleep(500 * time.Millisecond)
					st := fetchStatus()
					if st.Alive && st.Duration > 0 && st.Pos > 1 {
						ipcCmd([]any{"seek", seekTo[0], "absolute"})
						break
					}
				}
			}()
		}

		// Feedback
		header("streams")
		fmt.Printf("  %s\n\n", grey(sub))
		for i := start; i < end; i++ {
			fmt.Printf("  %s  %s\n", accent(fmt.Sprintf("[%2d]", i+1)), filtLabels[i])
		}
		blank()
		if wasPlaying {
			ok("added to playlist  " + grey("— mpv keeps playing"))
		} else {
			ok("opened in mpv  " + grey("— browse freely while it plays"))
		}
		StatusStart()
		time.Sleep(1200 * time.Millisecond)
		StatusStop()
	}
}

// ── Episode screen ────────────────────────────────────────────────────────────

func episodeScreen(addons []Addon, cfg AppConfig, m Meta, targetSeason int) navAct {
	header(m.Name)
	spin("fetching episode list...")
	sm := GetSeriesMeta(m)

	if len(sm.Videos) == 0 {
		fail("no episode data found")
		blank()
		hint("press enter to go back")
		readLine()
		return navBack
	}

	// Collect seasons
	seasonSet := map[int]bool{}
	for _, v := range sm.Videos {
		if v.Season > 0 {
			seasonSet[v.Season] = true
		}
	}
	seasons := make([]int, 0, len(seasonSet))
	for s := range seasonSet {
		seasons = append(seasons, s)
	}
	sort.Ints(seasons)

	reverseSeasons := false

	// If targetSeason set (from favs/history), jump straight there
	if targetSeason > 0 {
		act := epPickerScreen(addons, cfg, m, sm, seasons, targetSeason)
		if act == navBackAll {
			return navBackAll
		}
		// navBack from ep picker → fall into season picker below
		targetSeason = 0
	}

	// Season picker
	for {
		watched := WatchedEpisodes(m.ID)

		displaySeasons := make([]int, len(seasons))
		copy(displaySeasons, seasons)
		if reverseSeasons {
			for i, j := 0, len(displaySeasons)-1; i < j; i, j = i+1, j-1 {
				displaySeasons[i], displaySeasons[j] = displaySeasons[j], displaySeasons[i]
			}
		}

		items := make([]Item, len(displaySeasons))
		for i, s := range displaySeasons {
			// Count episodes and watched for this season
			total := 0
			watchedCount := 0
			for _, v := range sm.Videos {
				if v.Season == s {
					total++
					if watched[[2]int{s, v.Episode}] {
						watchedCount++
					}
				}
			}
			badge := fmt.Sprintf("%d ep", total)
			if watchedCount > 0 {
				badge += fmt.Sprintf("  %s", good(fmt.Sprintf("%d/%d ✓", watchedCount, total)))
			}
			allWatched := total > 0 && watchedCount == total
			items[i] = Item{
				Label:   bold(fmt.Sprintf("Season %d", s)),
				Badge:   badge,
				Watched: allWatched,
			}
		}

		res := ShowList(ListOpts{
			Title:    m.Name,
			Items:    items,
			CanBack:  true,
			CanFav:   true,
			CanSort:  len(seasons) > 1,
		})

		switch res.Act {
		case ActQuit:
			os.Exit(0)
		case ActBack:
			return navBack
		case ActFav:
			addFavFromMeta(m, 0)
		case ActSort:
			reverseSeasons = !reverseSeasons
		case ActPick:
			if res.Pick < 0 {
				fmt.Printf("\n  %s\n\n", bad(fmt.Sprintf("pick 1–%d", len(seasons))))
				time.Sleep(600 * time.Millisecond)
				continue
			}
			season := displaySeasons[res.Pick]
			act := epPickerScreen(addons, cfg, m, sm, seasons, season)
			if act == navBackAll {
				return navBackAll
			}
		}
	}
}

// epPickerScreen shows episodes for one season.
func epPickerScreen(addons []Addon, cfg AppConfig, m Meta, sm SeriesMeta, allSeasons []int, season int) navAct {
	reverseEps := false

	for {
		header(m.Name)
		spin(fmt.Sprintf("loading Season %d...", season))
		eps := GetSeasonEpisodes(m, season, sm, cfg.OmdbKey)
		watched := WatchedEpisodes(m.ID)

		if reverseEps {
			for i, j := 0, len(eps)-1; i < j; i, j = i+1, j-1 {
				eps[i], eps[j] = eps[j], eps[i]
			}
		}

		items := make([]Item, len(eps))
		for i, v := range eps {
			w := watched[[2]int{season, v.Episode}]

			// Check for partial progress
			if !w {
				pos, dur, _ := GetPosition(v.ID)
				if pos > 0 && dur > 0 && (pos/dur)*100 < 70 {
					badge := yell("▶ " + fmtSecs(pos))
					if r := fmtRelease(v.Released); r != "" {
						badge += "  " + grey(r)
					}
					item := epItem(v, false)
					item.Badge = badge
					items[i] = item
					continue
				}
			}
			items[i] = epItem(v, w)
		}

		// Build video ID map
		vidByEp := map[int]Video{}
		for _, v := range eps {
			vidByEp[v.Episode] = v
		}

		res := ShowList(ListOpts{
			Title:          m.Name,
			Sub:            fmt.Sprintf("Season %d  ·  %d episodes", season, len(eps)),
			Items:          items,
			CanBack:        true,
			CanBackAll:     true,
			CanFav:         true,
			CanSort:        true,
			CanToggleWatch: true,
		})

		switch res.Act {
		case ActQuit:
			os.Exit(0)
		case ActBack:
			return navBack
		case ActBackAll:
			return navBackAll
		case ActFav:
			addFavFromMeta(m, season)
		case ActSort:
			reverseEps = !reverseEps
		case ActToggleWatch:
			// Ask which episode
			header(m.Name)
			fmt.Printf("  %s\n\n", grey(fmt.Sprintf("Season %d — toggle watched", season)))
			for i, item := range items {
				fmt.Printf("  %s  %s\n", accent(fmt.Sprintf("[%2d]", i+1)), item.Label+"  "+item.Sub)
			}
			blank()
			fmt.Printf("  %s  %s: ", grey("toggle which"), grey("0=cancel"))
			n, err := strconv.Atoi(readLine())
			if err == nil && n >= 1 && n <= len(eps) {
				ep := eps[n-1]
				nowWatched := ToggleWatchedByEpisode(m.ID, season, ep.Episode)
				if nowWatched {
					ok(fmt.Sprintf("marked E%02d as watched", ep.Episode))
				} else {
					ok(fmt.Sprintf("marked E%02d as unwatched", ep.Episode))
				}
				time.Sleep(600 * time.Millisecond)
			}
		case ActPick:
			if res.Pick < 0 {
				fmt.Printf("\n  %s\n\n", bad(fmt.Sprintf("pick 1–%d", len(eps))))
				time.Sleep(600 * time.Millisecond)
				continue
			}
			ep := eps[res.Pick]

			// Resume prompt — check by video_id first, fall back to show+season+ep
			var seekPos float64
			savedPos, savedDur, _ := GetPosition(ep.ID)
			if savedPos == 0 {
				savedPos, savedDur, _ = GetPositionByEpisode(m.ID, season, ep.Episode)
			}
			if savedPos > 0 && savedDur > 0 {
				pct := (savedPos / savedDur) * 100
				if pct >= 5 {
					header(m.Name)
					fmt.Printf("  %s\n\n", grey(fmt.Sprintf("S%02dE%02d  %s", season, ep.Episode, ep.Title)))
					fmt.Printf("  %s  %s / %s\n\n",
						yell("▶ paused at"),
						bold(fmtSecs(savedPos)),
						fmtSecs(savedDur),
					)
					fmt.Printf("  %s  resume from %s\n", accent("[1]"), fmtSecs(savedPos))
					fmt.Printf("  %s  play from beginning\n", accent("[2]"))
					blank()
					fmt.Printf("  %s: ", grey("›"))
					StatusStart()
					choice := readLine()
					StatusStop()
					if choice != "2" {
						seekPos = savedPos
					}
				}
			}

			// Record history
			AddHistory(HistoryEntry{
				Name: m.Name, ID: m.ID, Type: m.Type,
				Source: m.Source, Year: m.Year,
				Season: season, Episode: ep.Episode,
				VideoID: ep.ID, EpTitle: ep.Title,
			}, cfg.HistoryMax)

			// Build episode context for [ / ] nav
			ctx := &EpCtx{
				Show:     m,
				Season:   season,
				Episodes: eps,
				Index:    res.Pick,
			}

			act := streamScreen(addons, cfg, "series", ep.ID, m, ctx, seekPos)

			if act == navBackAll {
				return navBackAll
			}
			// navBack from streams → back to episode picker
		}
	}
}

// ── Results screen ────────────────────────────────────────────────────────────

func resultsScreen(addons []Addon, cfg AppConfig, items []Meta, title, sub string) {
	listItems := make([]Item, len(items))
	for i, m := range items {
		listItems[i] = metaItem(m)
	}

	page := 0
	filter := ""

	for {
		// Apply filter
		var filtered []Meta
		var filtItems []Item
		fq := strings.ToLower(filter)
		for i, m := range items {
			if fq == "" || strings.Contains(strings.ToLower(m.Name), fq) {
				filtered = append(filtered, m)
				filtItems = append(filtItems, listItems[i])
			}
		}

		displaySub := sub
		if filter != "" {
			displaySub += "  ·  " + hi("/"+filter)
		}

		res := ShowList(ListOpts{
			Title:     title,
			Sub:       displaySub,
			Items:     filtItems,
			Page:      page,
			CanBack:   true,
			CanFav:    true,
			CanFilter: true,
		})

		switch res.Act {
		case ActQuit:
			os.Exit(0)
		case ActBack:
			return
		case ActNextPage:
			page++
		case ActPrevPage:
			if page > 0 {
				page--
			}
		case ActFilter:
			header(title)
			fmt.Printf("  %s\n\n", grey(sub))
			hint("filter by name — empty to clear")
			blank()
			fmt.Printf("  %s: ", grey("/"))
			filter = strings.TrimSpace(readLine())
			page = 0
		case ActFav:
			header(title)
			for i, item := range filtItems {
				fmt.Printf("  %s  %s\n", accent(fmt.Sprintf("[%2d]", i+1)), item.Label)
			}
			blank()
			fmt.Printf("  %s  %s: ", grey("favourite which"), grey("0=cancel"))
			n, err := strconv.Atoi(readLine())
			if err == nil && n >= 1 && n <= len(filtered) {
				addFavFromMeta(filtered[n-1], 0)
			}
		case ActPick:
			if res.Pick < 0 {
				fmt.Printf("\n  %s\n\n", bad("invalid pick"))
				time.Sleep(600 * time.Millisecond)
				continue
			}
			m := filtered[res.Pick]
			if m.Type == "movie" {
				AddHistory(HistoryEntry{
					Name: m.Name, ID: m.ID, Type: m.Type,
					Source: m.Source, Year: m.Year,
				}, cfg.HistoryMax)
				streamScreen(addons, cfg, "movie", m.ID, m, nil)
			} else {
				episodeScreen(addons, cfg, m, 0)
			}
		}
	}
}

// ── Browse screen ─────────────────────────────────────────────────────────────

func browseScreen(addons []Addon, cfg AppConfig, source string) {
	title := map[string]string{"movie": "movies", "show": "shows", "anime": "anime"}[source]
	page := 0
	filter := ""

	// Load all items once into cache
	header(title)
	spin("loading...")
	Browse(source, 0, 10) // triggers cache population
	allItems := cacheBrowse[source+":all"]

	// Cache filtered results so redraws don't re-filter the whole list
	var filtered []Meta
	lastFilter := "~UNSET~" // sentinel so first run always filters

	for {
		perPage := contentRows(true)

		// Only re-filter when filter string changes
		if filter != lastFilter {
			filtered = filtered[:0]
			fq := strings.ToLower(filter)
			for _, m := range allItems {
				if fq == "" || strings.Contains(strings.ToLower(m.Name), fq) {
					filtered = append(filtered, m)
				}
			}
			lastFilter = filter
		}

		if len(filtered) == 0 && filter == "" {
			fail("no results")
			blank()
			hint("press enter to go back")
			readLine()
			return
		}

		// Paginate filtered results
		start := page * perPage
		if start >= len(filtered) && page > 0 {
			page = 0
			start = 0
		}
		end := start + perPage
		hasMore := end < len(filtered)
		if end > len(filtered) {
			end = len(filtered)
		}
		pageItems := filtered[start:end]

		listItems := make([]Item, len(pageItems))
		for i, m := range pageItems {
			listItems[i] = metaItem(m)
		}

		totalPages := (len(filtered) + perPage - 1) / perPage
		if totalPages < 1 {
			totalPages = 1
		}
		displaySub := fmt.Sprintf("top %ss right now", source)
		if filter != "" {
			displaySub = hi("/"+filter) + grey(fmt.Sprintf("  (%d results)", len(filtered)))
		}
		if totalPages > 1 {
			displaySub += grey(fmt.Sprintf("  page %d/%d", page+1, totalPages))
		}

		res := ShowList(ListOpts{
			Title:     title,
			Sub:       displaySub,
			Items:     listItems,
			CanBack:   true,
			CanFav:    true,
			CanFilter: true,
		})

		switch res.Act {
		case ActQuit:
			os.Exit(0)
		case ActBack:
			return
		case ActNextPage:
			if hasMore {
				page++
			}
		case ActPrevPage:
			if page > 0 {
				page--
			}
		case ActFilter:
			header(title)
			hint("filter by name — empty to show all")
			blank()
			fmt.Printf("  %s: ", grey("/"))
			filter = strings.TrimSpace(readLine())
			page = 0
		case ActFav:
			header(title)
			for i, item := range listItems {
				fmt.Printf("  %s  %s\n", accent(fmt.Sprintf("[%2d]", i+1)), item.Label)
			}
			blank()
			fmt.Printf("  %s  %s: ", grey("favourite which"), grey("0=cancel"))
			n, err := strconv.Atoi(readLine())
			if err == nil && n >= 1 && n <= len(pageItems) {
				addFavFromMeta(pageItems[n-1], 0)
			}
		case ActPick:
			if res.Pick < 0 {
				fmt.Printf("\n  %s\n\n", bad(fmt.Sprintf("pick 1–%d", len(pageItems))))
				time.Sleep(600 * time.Millisecond)
				continue
			}
			m := pageItems[res.Pick]
			if m.Type == "movie" {
				AddHistory(HistoryEntry{
					Name: m.Name, ID: m.ID, Type: m.Type,
					Source: m.Source, Year: m.Year,
				}, cfg.HistoryMax)
				streamScreen(addons, cfg, "movie", m.ID, m, nil)
			} else {
				episodeScreen(addons, cfg, m, 0)
			}
		}
	}
}

// ── Continue watching ─────────────────────────────────────────────────────────

// lastInProgress returns the most recently watched partial entry, or nil.
func lastInProgress() *HistoryEntry {
	// Return cached value if still valid
	if inProgressValid {
		return inProgressCache
	}

	h := LoadHistory()
	inProgressCache = nil
	for i := range h.Items {
		e := &h.Items[i]
		if e.Position > 0 && e.Duration > 0 && !e.Watched {
			pct := (e.Position / e.Duration) * 100
			if pct >= 5 && pct <= 90 {
				inProgressCache = e
				break
			}
		}
	}
	inProgressValid = true
	return inProgressCache
}

// continueWatching jumps straight to the stream picker for the last in-progress item.
func continueWatching(addons []Addon, cfg AppConfig) {
	e := lastInProgress()
	if e == nil {
		return
	}
	m := Meta{ID: e.ID, Type: e.Type, Name: e.Name, Year: e.Year, Source: e.Source}

	var ctx *EpCtx
	if e.Type != "movie" && e.Season > 0 && e.VideoID != "" {
		sm := GetSeriesMeta(m)
		eps := GetSeasonEpisodes(m, e.Season, sm, cfg.OmdbKey)
		idx := 0
		for i, v := range eps {
			if v.Episode == e.Episode {
				idx = i
				break
			}
		}
		ctx = &EpCtx{Show: m, Season: e.Season, Episodes: eps, Index: idx}
	}

	videoID := e.ID
	if e.VideoID != "" {
		videoID = e.VideoID
	}
	mediaType := "movie"
	if e.Type == "series" {
		mediaType = "series"
	}

	act := streamScreen(addons, cfg, mediaType, videoID, m, ctx, e.Position)
	_ = act

	// b from stream screen — go to episode picker
	if act == navBack && e.Type == "series" && e.Season > 0 {
		episodeScreen(addons, cfg, m, e.Season)
	}
}

// ── Search screen ─────────────────────────────────────────────────────────────

func searchScreen(addons []Addon, cfg AppConfig) {
	for {
		header("search")
		fmt.Printf("  %s\n\n", grey("movies · shows · anime"))
		hint("empty = back")
		blank()
		fmt.Printf("  %s: ", grey("›"))
		StatusStart()
		query := readLine()
		StatusStop()

		if query == "" {
			return
		}

		header("search")
		spin(fmt.Sprintf("searching '%s'...", query))
		results := Search(query, cfg.OmdbKey)
		if len(results) > 25 {
			results = results[:25]
		}

		if len(results) == 0 {
			blank()
			fail("no results")
			blank()
			hint("press enter to search again")
			readLine()
			continue
		}

		resultsScreen(addons, cfg, results, "results",
			fmt.Sprintf("'%s'  ·  %d result(s)", query, len(results)))
	}
}

// ── Favourites screen ─────────────────────────────────────────────────────────

func favsScreen(addons []Addon, cfg AppConfig) {
	page := 0
	for {
		fl := LoadFavs()
		header("favourites")

		if len(fl.Items) == 0 {
			blank()
			fmt.Printf("  %s\n", grey("no favourites yet — press f while browsing to add"))
			blank()
			hint("press enter to go back")
			readLine()
			return
		}

		items := make([]Item, len(fl.Items))
		for i, f := range fl.Items {
			items[i] = FavItem(f)
		}

		res := ShowList(ListOpts{
			Title:     "favourites",
			Items:     items,
			Page:      page,
			CanBack:   true,
			CanDelete: true,
		})

		switch res.Act {
		case ActQuit:
			os.Exit(0)
		case ActBack:
			return
		case ActNextPage:
			page++
		case ActPrevPage:
			if page > 0 {
				page--
			}
		case ActDelete:
			header("favourites")
			for i, item := range items {
				fmt.Printf("  %s  %s\n", accent(fmt.Sprintf("[%2d]", i+1)), item.Label)
			}
			blank()
			fmt.Printf("  %s  %s: ", grey("remove which"), grey("0=cancel"))
			n, err := strconv.Atoi(readLine())
			if err == nil && n >= 1 && n <= len(fl.Items) {
				name := fl.Items[n-1].Name
				RemoveFav(n - 1)
				ok(fmt.Sprintf("removed %s", bold(name)))
				time.Sleep(600 * time.Millisecond)
			}
		case ActPick:
			if res.Pick < 0 {
				fmt.Printf("\n  %s\n\n", bad(fmt.Sprintf("pick 1–%d or d to remove", len(fl.Items))))
				time.Sleep(600 * time.Millisecond)
				continue
			}
			f := fl.Items[res.Pick]
			m := Meta{ID: f.ID, Type: f.Type, Name: f.Name, Year: f.Year, Source: f.Source}
			if f.Type == "movie" {
				streamScreen(addons, cfg, "movie", f.ID, m, nil)
			} else {
				episodeScreen(addons, cfg, m, f.Season)
			}
		}
	}
}

// ── History screen ────────────────────────────────────────────────────────────

func histScreen(addons []Addon, cfg AppConfig) {
	page := 0
	filter := ""

	for {
		h := LoadHistory()
		header("history")

		if len(h.Items) == 0 {
			blank()
			fmt.Printf("  %s\n", grey("nothing watched yet"))
			blank()
			hint("press enter to go back")
			readLine()
			return
		}

		// Apply filter
		var filtered []HistoryEntry
		fq := strings.ToLower(filter)
		for _, e := range h.Items {
			if fq == "" || strings.Contains(strings.ToLower(e.Name), fq) {
				filtered = append(filtered, e)
			}
		}

		items := make([]Item, len(filtered))
		for i, e := range filtered {
			items[i] = HistoryItem(e)
		}

		displaySub := ""
		if filter != "" {
			displaySub = hi("/"+filter) + grey(fmt.Sprintf("  (%d results)", len(filtered)))
		}

		res := ShowList(ListOpts{
			Title:        "history",
			Sub:          displaySub,
			Items:        items,
			Page:         page,
			CanBack:      true,
			CanDelete:    true,
			CanDeleteAll: true,
			CanFilter:    true,
		})

		switch res.Act {
		case ActQuit:
			os.Exit(0)
		case ActBack:
			return
		case ActNextPage:
			page++
		case ActPrevPage:
			if page > 0 {
				page--
			}
		case ActFilter:
			header("history")
			hint("filter by title — empty to show all")
			blank()
			fmt.Printf("  %s: ", grey("/"))
			filter = strings.TrimSpace(readLine())
			page = 0
		case ActDelete:
			header("history")
			for i, item := range items {
				fmt.Printf("  %s  %s\n", accent(fmt.Sprintf("[%2d]", i+1)), item.Label)
			}
			blank()
			fmt.Printf("  %s  %s: ", grey("remove which"), grey("0=cancel"))
			n, err := strconv.Atoi(readLine())
			if err == nil && n >= 1 && n <= len(filtered) {
				// Find original index
				target := filtered[n-1]
				for i, e := range h.Items {
					if e.ID == target.ID && e.Season == target.Season && e.Episode == target.Episode {
						ClearHistoryEntry(i)
						ok(fmt.Sprintf("removed %s", bold(target.Name)))
						time.Sleep(600 * time.Millisecond)
						break
					}
				}
			}
		case ActDeleteAll:
			header("history")
			blank()
			fmt.Printf("  %s  %s: ", grey("clear all history?"), grey("y=yes  n=no"))
			if strings.ToLower(readLine()) == "y" {
				ClearAllHistory()
				ok("history cleared")
				time.Sleep(600 * time.Millisecond)
				return
			}
		case ActPick:
			if res.Pick < 0 {
				fmt.Printf("\n  %s\n\n", bad(fmt.Sprintf("pick 1–%d", len(filtered))))
				time.Sleep(600 * time.Millisecond)
				continue
			}
			e := filtered[res.Pick]
			m := Meta{ID: e.ID, Type: e.Type, Name: e.Name, Year: e.Year, Source: e.Source}

			if e.Type == "movie" {
				streamScreen(addons, cfg, "movie", e.ID, m, nil, e.Position)
			} else if e.Season > 0 && e.VideoID != "" {
				// Jump to that episode's stream picker with full context
				eps := GetSeasonEpisodes(m, e.Season, GetSeriesMeta(m), cfg.OmdbKey)
				idx := 0
				for i, v := range eps {
					if v.Episode == e.Episode {
						idx = i
						break
					}
				}
				ctx := &EpCtx{Show: m, Season: e.Season, Episodes: eps, Index: idx}
				act := streamScreen(addons, cfg, "series", e.VideoID, m, ctx, e.Position)
				if act == navBack {
					episodeScreen(addons, cfg, m, e.Season)
				}
			} else {
				episodeScreen(addons, cfg, m, 0)
			}
		}
	}
}

// ── Main menu ─────────────────────────────────────────────────────────────────

func mainMenu(addons []Addon, cfg AppConfig) {
	for {
		header("")
		blank()
		// Continue watching line — shown at top if something is in progress
		if ip := lastInProgress(); ip != nil {
			title := ip.Name
			if ip.Season > 0 && ip.Episode > 0 {
				title += fmt.Sprintf(" · S%02dE%02d", ip.Season, ip.Episode)
			}
			remaining := ip.Duration - ip.Position
			fmt.Printf("  %s  %s  %s\n",
				accent("[w]"),
				bold("continue")+"  "+grey(title),
				grey(fmtSecs(ip.Position)+" / "+fmtSecs(ip.Duration)+"  "+fmtSecs(remaining)+" left"),
			)
			blank()
		}
		fmt.Printf("  %s  %s\n", accent("[m]"), bold("movies")+"     "+grey("top right now"))
		fmt.Printf("  %s  %s\n", accent("[s]"), bold("shows")+"      "+grey("top right now"))
		fmt.Printf("  %s  %s\n", accent("[a]"), bold("anime")+"      "+grey("top right now"))
		blank()
		fmt.Printf("  %s  %s\n", accent("[/]"), bold("search")+"     "+grey("movies · shows · anime"))
		fmt.Printf("  %s  %s\n", accent("[f]"), bold("favourites")+"  "+grey("your saved titles"))
		fmt.Printf("  %s  %s\n", accent("[h]"), bold("history")+"    "+grey("recently watched"))
		fmt.Printf("  %s  %s\n", accent("[d]"), bold("library")+"    "+grey("dmm · torrentio · your RD content"))
		fmt.Printf("  %s  %s\n", accent("[c]"), bold("config")+"     "+grey("settings"))
		blank()

		// Now playing line
		if st := fetchStatus(); st.Alive && st.Duration > 0 {
			t := cleanTitle(st.Title)
			if t != "" {
				runes := []rune(t)
				if len(runes) > 50 {
					t = string(runes[:50]) + "…"
				}
				fmt.Printf("  %s  %s  %s / %s (%.0f%%)\n",
					accent("▶"),
					grey(t),
					bold(fmtSecs(st.Pos)),
					fmtSecs(st.Duration),
					st.Percent,
				)
				blank()
			}
		}

		fmt.Printf("  %s\n", grey("0=quit"))
		blank()
		fmt.Printf("  %s: ", grey("›"))
		StatusStart()
		choice := readLine()
		StatusStop()

		switch strings.ToLower(choice) {
		case "0", "q":
			return
		case "m":
			browseScreen(addons, cfg, "movie")
		case "s":
			browseScreen(addons, cfg, "show")
		case "a":
			browseScreen(addons, cfg, "anime")
		case "/":
			searchScreen(addons, cfg)
		case "f":
			favsScreen(addons, cfg)
		case "h":
			histScreen(addons, cfg)
		case "d":
			libraryScreen(addons, cfg)
		case "c":
			configScreen(&cfg)
		case "w":
			continueWatching(addons, cfg)
		default:
			fmt.Printf("\n  %s\n", bad("use m, s, a, /, f, h, d, w or c"))
			time.Sleep(600 * time.Millisecond)
		}
	}
}
