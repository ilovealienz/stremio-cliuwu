package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

func LoadFavs() FavouriteList {
	b, err := os.ReadFile(favsFile())
	if err != nil {
		return FavouriteList{}
	}
	var fl FavouriteList
	json.Unmarshal(b, &fl)
	return fl
}

func saveFavs(fl FavouriteList) {
	ensureDir()
	b, _ := json.MarshalIndent(fl, "", "  ")
	os.WriteFile(favsFile(), b, 0644)
}

func AddFav(f Favourite) {
	fl := LoadFavs()
	f.Added = time.Now().Format("2006-01-02")
	for i, ex := range fl.Items {
		if ex.ID == f.ID && ex.Season == f.Season {
			fl.Items[i] = f
			saveFavs(fl)
			return
		}
	}
	fl.Items = append(fl.Items, f)
	saveFavs(fl)
}

func RemoveFav(idx int) {
	fl := LoadFavs()
	if idx < 0 || idx >= len(fl.Items) {
		return
	}
	fl.Items = append(fl.Items[:idx], fl.Items[idx+1:]...)
	saveFavs(fl)
}

// FavItem builds a list Item for a favourite, including watch progress badge.
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
