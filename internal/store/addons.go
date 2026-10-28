package store

import (
	"fmt"

	"stremio-cliuwu/internal/stremio"
)

// Addons are now plain manifest URLs kept in addons.json. No account, no
// authKey, no vault — you paste the same URLs you'd paste into Stremio:
//
//	https://v3-cinemeta.strem.io/manifest.json
//	https://torrentio.strem.fun/torbox=<key>/manifest.json
//
// Anything the old GetAddons() call returned was just this list anyway.

const defaultCinemeta = "https://v3-cinemeta.strem.io/manifest.json"

const defaultKitsu = "https://anime-kitsu.strem.fun/manifest.json"

// The official subtitles addon. Seeded because subtitles are the one thing
// you'd otherwise have to know to go looking for — a missing stream is
// obvious immediately, a missing subtitle only becomes obvious mid-episode.
const defaultOpenSubtitles = "https://opensubtitles-v3.strem.io/manifest.json"

// ── Persistence ───────────────────────────────────────────────────────────────

func LoadAddonRefs() stremio.AddonList {
	var al stremio.AddonList
	if err := readJSON(AddonsFile(), &al); err != nil {
		return stremio.AddonList{}
	}
	return al
}

func SaveAddonRefs(al stremio.AddonList) error { return writeJSON(AddonsFile(), al) }

// SeedAddons writes the two metadata addons every install needs. Called once
// when addons.json doesn't exist yet.
func SeedAddons() stremio.AddonList {
	al := stremio.AddonList{Items: []stremio.AddonRef{
		{URL: defaultCinemeta},
		{URL: defaultKitsu},
		{URL: defaultOpenSubtitles},
	}}
	SaveAddonRefs(al)
	return al
}

func AddAddonRef(raw string) (string, error) {
	u, err := stremio.NormalizeManifestURL(raw)
	if err != nil {
		return "", err
	}
	al := LoadAddonRefs()
	for _, it := range al.Items {
		if it.URL == u {
			return u, fmt.Errorf("already added")
		}
	}
	al.Items = append(al.Items, stremio.AddonRef{URL: u})
	return u, SaveAddonRefs(al)
}

func RemoveAddonRef(idx int) {
	al := LoadAddonRefs()
	if idx < 0 || idx >= len(al.Items) {
		return
	}
	al.Items = append(al.Items[:idx], al.Items[idx+1:]...)
	SaveAddonRefs(al)
}

func ToggleAddonRef(idx int) {
	al := LoadAddonRefs()
	if idx < 0 || idx >= len(al.Items) {
		return
	}
	al.Items[idx].Disabled = !al.Items[idx].Disabled
	SaveAddonRefs(al)
}

// MoveAddonRef shifts an entry by delta. Order matters: stream results are
// grouped in addon order, so this is how you put your debrid addon on top.
func MoveAddonRef(idx, delta int) int {
	al := LoadAddonRefs()
	j := idx + delta
	if idx < 0 || idx >= len(al.Items) || j < 0 || j >= len(al.Items) {
		return idx
	}
	al.Items[idx], al.Items[j] = al.Items[j], al.Items[idx]
	SaveAddonRefs(al)
	return j
}
