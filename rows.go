package main

import (
	"fmt"
	"strings"
)

// Every list row is built here.
//
// These used to live beside the code that loads the data — HistoryItem in
// history.go, FavItem in favs.go — which meant the storage layer reached into
// the styling layer to produce ANSI. Wrong direction, and the thing that would
// block ever splitting this into packages.

func metaItem(m Meta) Item {
	if m.Name == "" {
		m.Name = m.ID // some debrid catalogs omit names entirely
	}
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
	badge := ""
	if r := fmtRelease(v.Released); r != "" {
		badge = grey(r)
	}

	it := Item{
		Label:   bold(hi(fmt.Sprintf("E%02d", v.Episode))),
		Sub:     v.Title,
		Badge:   badge,
		Watched: watched,
	}

	// Catalogs list episodes well before they air. Without a marker they look
	// identical to everything else, and you only find out when the stream
	// search comes back empty.
	if !videoAired(v) {
		// The date, same as an aired episode — a row is for scanning, and
		// switching format between aired and unaired broke the column. How
		// far off it is belongs in the info panel, where there's room to say
		// it properly.
		it.Badge = stWarn.Render("○ " + fmtRelease(v.Released))
		it.Dim = true
	}
	return it
}

func HistoryItem(e HistoryEntry) Item {
	yr := e.Year
	if yr == "" {
		yr = "?"
	}
	ep := ""
	if e.Season > 0 && e.Episode > 0 {
		ep = fmt.Sprintf("  S%02dE%02d", e.Season, e.Episode)
		if e.EpTitle != "" {
			ep += "  " + e.EpTitle
		}
	}
	label := bold(e.Name) + grey("  ("+yr+")") + ep

	badge := sourceTag(e.Source)
	if e.Watched {
		badge += "  " + good("✓")
	} else if e.Position > 0 && e.Duration > 0 {
		badge += "  " + yell("▶ "+fmtSecs(e.Position))
	}

	kind := "dmy"
	if ctx != nil {
		kind = ctx.cfg.DateFormat
	}
	badge += grey("  " + e.WatchedAt.Format(dateLayout(kind)))

	return Item{Label: label, Badge: badge, Watched: e.Watched}
}

func FavItem(f Favourite) Item {
	yr := f.Year
	if yr == "" {
		yr = "?"
	}
	season := ""
	if f.Season > 0 {
		season = fmt.Sprintf("  S%02d", f.Season)
	}
	label := fmt.Sprintf("%s  %s", bold(f.Name), grey("("+yr+")"))

	// Get watch progress from history
	badge := sourceTag(f.Source) + season
	if f.Type == "series" {
		h := LoadHistory()
		var lastEntry *HistoryEntry
		for i := range h.Items {
			e := &h.Items[i]
			if e.ID == f.ID {
				if f.Season == 0 || e.Season == f.Season {
					lastEntry = e
					break
				}
			}
		}
		if lastEntry != nil {
			if lastEntry.Watched {
				badge += "  " + good("✓")
			} else if lastEntry.Position > 0 && lastEntry.Duration > 0 {
				badge += "  " + yell("▶ "+fmtSecs(lastEntry.Position))
			} else if lastEntry.Season > 0 && lastEntry.Episode > 0 {
				badge += "  " + grey(fmt.Sprintf("S%02dE%02d", lastEntry.Season, lastEntry.Episode))
			}
		}
	}

	return Item{Label: label, Badge: badge}
}

// AddonItem renders an addon row for the addons screen.
func AddonItem(ref AddonRef, a *Addon) Item {
	name := RedactURL(ref.URL)
	sub := ""
	badge := ""

	switch {
	case a == nil:
		badge = grey("…")
	case a.Err != nil:
		badge = bad("failed")
		sub = a.Err.Error()
	default:
		name = a.Manifest.Name
		var caps []string
		if a.HasStreams() {
			caps = append(caps, "streams")
		}
		if len(a.Manifest.Catalogs) > 0 {
			caps = append(caps, fmt.Sprintf("%d catalog(s)", len(a.Manifest.Catalogs)))
		}
		sub = strings.Join(caps, " · ")
		if a.Manifest.Version != "" {
			badge = grey("v" + a.Manifest.Version)
		}
	}

	if ref.Disabled {
		badge = grey("off")
	}
	return Item{Label: bold(name), Sub: sub, Badge: badge, Dim: ref.Disabled}
}
