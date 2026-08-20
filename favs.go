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
