package main

import (
	"encoding/json"
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
	_ = writeJSON(favsFile(), fl)
}

// ToggleFav adds a favourite, or removes it if it is already there.
// Reports whether it is favourited afterwards.
//
// Keyed on id and season together: a season is favourited separately from its
// show, so pressing f on season 3 should not clear the show itself.
func ToggleFav(f Favourite) bool {
	fl := LoadFavs()
	for i, ex := range fl.Items {
		if ex.ID == f.ID && ex.Season == f.Season {
			fl.Items = append(fl.Items[:i], fl.Items[i+1:]...)
			saveFavs(fl)
			return false
		}
	}

	f.Added = time.Now().Format("2006-01-02")
	fl.Items = append(fl.Items, f)
	saveFavs(fl)
	return true
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
