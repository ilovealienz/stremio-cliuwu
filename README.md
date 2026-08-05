# stremio-cliuwu

A terminal Stremio client. Bubble Tea TUI, mpv for playback, no account.

## What changed in 0.3.0

**Catalogs and search come from your addons.** Browsing used to hit Cinemeta's
`top.json` and four fixed Kitsu catalog IDs; search hit three hardcoded URLs.
Installing an addon could not add a catalog or extend search. Now the menu is
built from `catalogs[]` in the installed manifests, and search fans out across
every catalog whose `extra` declares `search` support.

`movies` / `shows` / `anime` still exist as entry points and still take
`m`/`s`/`a`, but they now list the catalogs of that type — skipping straight
into it when there's only one. Buckets with no catalogs behind them are hidden.

Meta lookups follow the same rule: instead of guessing between Cinemeta and
Kitsu from a source tag, we ask whichever addons declare a `meta` resource for
that id. Catalog rows also read `releaseInfo` directly rather than firing one
HTTP request per row just to display a year.

**Cached debrid streams sort first.** Torrentio marks instantly-available
results with `⚡` and a `[RD+]`-style tag; `⏳` means it would have to download
first. Picking an uncached result means waiting on the provider before mpv gets
anything, so cached beats resolution in the sort order. Toggleable.

**Stream cache expires.** It was an unbounded map with no TTL, so a debrid URL
resolved an hour ago would be replayed after the provider had already expired
it. Now 5 minutes, capped, with `R` on the stream screen to force a refetch.

**Finishing an episode opens the next one's stream list** rather than picking a
stream itself. The player can't tell cached from uncached, and a blind pick
that fails left you with an error toast and no list to retry from.

## What changed in 0.2.0

**No login.** The Stremio account is gone — no `api.strem.io`, no authKey, no
argon2id vault, no machine-bound encryption. You add addons the same way you'd
add them in Stremio: by manifest URL.

```
https://v3-cinemeta.strem.io/manifest.json
https://torrentio.strem.fun/torbox=<your-key>/manifest.json
```

Cinemeta and Kitsu are seeded on first run. Everything else is yours to add
from the `addons` screen (`a` to add, `t` to enable/disable, `J`/`K` to reorder
— stream results are grouped in list order).

Those URLs often embed a debrid API key, so `addons.json` is written `0600` and
the TUI redacts keys when it displays a URL.

**mpv uses `replace`, not `append-play`.** The old client appended to an mpv
playlist and then had to work out what was playing by asking mpv for its
current path and matching it against a URL→videoID map. With `replace` there's
exactly one file loaded, so the playing item is a single struct field, and
position/watched tracking has nothing to guess at.

The tradeoff: mpv's playlist was doing next-episode autoplay for free. That now
lives in Go — on `end-file` with `reason: eof` the player resolves the next
episode's streams and issues another `replace`. Toggle it under settings.

**IPC is event-driven.** One persistent connection with a reader goroutine and
`observe_property` on `time-pos`/`duration`/`pause`/`media-title`/
`paused-for-cache`, rather than reconnecting once a second to poll five
properties. Updates arrive as Bubble Tea messages; the status bar is part of
the normal render tree instead of raw cursor-save/restore escapes fighting the
rest of the output.

mpv is spawned with `--no-terminal`. Not optional — we're in the alt screen and
anything mpv prints to the tty shreds the UI.

## Layout

```
main.go              startup, wiring
app.go               root model, screen stack, chrome, status bar
list.go              the one list widget everything uses
widgets.go           spinner, prompt screen, confirm screen
screens_home.go      main menu + continue watching
screens_catalog.go   browse / search / results
screens_series.go    seasons, episodes
screens_streams.go   stream picker → PlayRequest
screens_manage.go    favourites, history, addons, settings
library.go           debrid catalog browsing (DMM / Torrentio cloud)
player.go            mpv IPC
addons.go            manifest store + fetching
api.go               Cinemeta / Kitsu / OMDB
streams.go           stream label formatting + sorting
history.go favs.go   persistence
paths.go config.go   config dir, config load/save, mpv detection
```

## Keys

Global:

| key | |
|---|---|
| `space` | pause / unpause |
| `,` `.` | seek ∓10s |
| `<` `>` | seek ∓60s |
| `X` | stop |
| `b` | back one screen |
| `ctrl+q` | quit |

Lists: `j`/`k` or arrows, `g`/`G`, `ctrl+u`/`ctrl+d`, `/` to filter, `enter` to
open, `b` or `esc` to go back, `q` back to the menu.

mpv is closed when you quit. Turn that off under settings if you'd rather
playback survive the TUI.

Screen-specific hints are in the footer.

## Config

`~/.config/stremio-cliuwu/` (or `$XDG_CONFIG_HOME`, or `%APPDATA%`):

| file | |
|---|---|
| `config.json` | mpv path, preferred quality, subtitle lang, autoplay |
| `addons.json` | manifest URLs — mode 0600, treat as secrets |
| `favourites.json` | |
| `history.json` | positions and watched state |

## Build

```
make run      # go mod tidy && go run .
make build
make install
```
