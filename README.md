# stremio-cliuwu

Disclaimer: MADE WITH CLAUDE FOR LINUX, IT KINDA WORKS ON WINDOWS. I'll try fix it at some point to get ipc to work for windows

A terminal interface for Stremio. browse, search, and stream movies, shows, and anime via mpv.

```
  ▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓
  stremio-cliuwu
  ▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓

  [w]  continue     Death Note · S01E02  04:21 / 23:11  18min left

  [m]  movies       top right now
  [s]  shows        top right now
  [a]  anime        top right now

  [/]  search       movies · shows · anime
  [f]  favourites   your saved titles
  [h]  history      recently watched
  [c]  config       settings

  ›: _
  ─────────────────────────────────────────
  Death Note - 02 - Confrontation.mkv  ▶ 04:21 / 23:11 (19%)  Cache: 45s
```

## Features

- Browse top movies, shows, and anime
- Search across all three sources
- Stream via any Stremio addon (Torrentio, Orion, AIOStreams, etc.)
- mpv playlist. pick multiple streams, browse freely while playing
- Resume playback from where you left off
- Watched tracking. auto-marks at 70%, manual toggle with `w`
- Favourites with watch progress shown
- History with position tracking
- Live status bar at the bottom of every screen
- `[` / `]` to jump between episodes in the stream picker
- Filter streams by addon or quality with `/`
- Rescan streams with `R`
- Config screen for all settings

## Requirements

- A [Stremio](https://www.stremio.com) account with addons installed
- [mpv](https://mpv.io) installed

## Installation

### Pre-built binaries

Download the latest release for your platform from the [releases page](../../releases).

```bash
# Linux
chmod +x stremio-cliuwu-linux
./stremio-cliuwu-linux

# Or install to PATH
sudo install -m755 stremio-cliuwu-linux /usr/local/bin/stremio-cliuwu
```

### Build from source

Requires [Go 1.21+](https://go.dev/dl/)

```bash
git clone https://github.com/yourusername/stremio-cliuwu
cd stremio-cliuwu
make build
./stremio-cliuwu
```

## First run

On first launch you'll be asked to:

1. **Set a vault password** encrypts your Stremio login using argon2id + AES-256-GCM. Machine-bound, never asked again on the same machine.
2. **Locate mpv** auto-detected from PATH. Enter the path manually if not found.
3. **Log in to Stremio** email + password, or paste your authKey.

To get your authKey from the Stremio web app:
1. Open [web.stremio.com](https://web.stremio.com)
2. Press F12 → Console
3. Run: `JSON.parse(localStorage.getItem("profile")).auth.key`

## Keybindings

### All screens
| Key | Action |
|-----|--------|
| `0` or `q` | Quit |
| `b` | Back one level |
| `B` | Back to list (favs/history/results) |
| `f` | Favourite |
| `r` | Reverse sort order |

### Stream picker
| Key | Action |
|-----|--------|
| `[` | Previous episode |
| `]` | Next episode |
| `/` | Filter streams |
| `c` | Clear filter |
| `R` | Rescan streams |
| `r` | Reverse order |
| `n` / `p` | Next / previous page |

### Episode picker
| Key | Action |
|-----|--------|
| `w` | Toggle watched status |
| `r` | Reverse episode order |

### History
| Key | Action |
|-----|--------|
| `d` | Remove entry |
| `D` | Clear all history |
| `/` | Filter by title |

### Favourites
| Key | Action |
|-----|--------|
| `d` | Remove entry |

## Config files

All config is stored in `~/.config/stremio-cliuwu/` on Linux/Mac, `%APPDATA%\stremio-cliuwu\` on Windows.

| File | Contents |
|------|----------|
| `auth.enc` | Encrypted Stremio auth (argon2id + AES-256-GCM, machine-bound) |
| `config.json` | App settings (mpv path, quality preference, subtitle language, etc.) |
| `favourites.json` | Saved titles |
| `history.json` | Watch history with positions |

To reset and re-login:
```bash
rm ~/.config/stremio-cliuwu/auth.enc
stremio-cliuwu
```

## Config options

Accessible via `[c]` in the main menu:

| Setting | Description |
|---------|-------------|
| mpv path | Path to mpv binary (auto-detected by default) |
| preferred quality | Sorts matching streams to the top (e.g. `1080P`, `4K`) |
| subtitle language | Passed to mpv as `--slang` (e.g. `en`, `ja`) |
| history max | Maximum history entries (default: 100) |
| OMDB API key | For episode titles and release dates. |

## mpv integration

Streams open in mpv with `--idle=yes` so the window stays open between tracks. The app communicates with mpv via a Unix socket at `/tmp/stremio-cliuwu.sock`.

You can pick multiple streams and they'll queue in mpv's playlist. Browse freely while mpv plays in the background. the status bar at the bottom updates live.

## Building for all platforms

```bash
make build-all
# outputs: dist/stremio-cliuwu-linux
#          dist/stremio-cliuwu.exe
#          dist/stremio-cliuwu-mac
```
