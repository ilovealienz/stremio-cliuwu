package poster

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"hash/fnv"
	"image"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"stremio-cliuwu/internal/store"
)

// Kitty inline posters, with half-block fallback.
//
// The half-block renderer in poster.go is plain text, so Bubble Tea can diff
// it — but one cell only holds two pixels, which is inherently blurry. On
// terminals that implement the unicode placeholder part of the kitty
// graphics protocol we show the real image instead.
//
// Only kitty and Ghostty do. WezTerm implements the graphics protocol but
// not placeholders, so it would take the transmit, render the placeholder
// cells as nothing, and skip the half-block path — worse than no support
// at all. Konsole is the same, on the older protocol.
//
// It works in two halves:
//
//   - The View string holds ordinary text cells: U+10EEEE plus combining
//     diacritics for row/column and the image id in the foreground colour.
//     Those measure exactly one column each, so layout, scrolling and resize
//     all behave like text.
//   - The actual pixels travel out of band: raw APC escape bytes written
//     straight to stdout from a tea.Cmd, never embedded in the View string.
//     A transmit (a=t, no visible placement) stores the image invisibly,
//     and a virtual placement (a=p,U=1) binds it to the placeholder cells.
//     Placeholders drawn before the image exists stay blank, so the View
//     only emits them after the transmit command reports back.
//
// Everything runs quiet (q=2): the terminal sends no responses, which matters
// because response bytes arriving on stdin would land in Bubble Tea's input.
// Inside tmux the raw bytes ride DCS passthrough, so a Kitty-family outer
// terminal works too (with allow-passthrough on). On anything else — unknown
// TERM, dumb terminals — the pane keeps the existing half-block path
// untouched.
//
// See https://sw.kovidgoyal.net/kitty/graphics-protocol/#unicode-placeholders

// kittyPlaceholder marks a cell as part of a kitty image.
const kittyPlaceholder = '\U0010EEEE'

// kittyDiacritic maps a small integer to its combining character. Row,
// column and id-high-byte values for posters stay under 256; the table in
// kitty_diacritics.go holds ~300 entries, anything beyond wraps to zero.
func kittyDiacritic(i int) rune {
	if i < 0 || i >= len(kittyDiacritics) {
		return kittyDiacritics[0]
	}
	return kittyDiacritics[i]
}

var (
	kittyOnce     sync.Once
	kittyDetected bool // what the environment suggests
	kittyTmux     bool // inside tmux: wrap raw output in DCS passthrough
)

// KittySupported reports whether the terminal is expected to speak the kitty
// graphics protocol. Checked once — the terminal doesn't change mid-run.
func KittySupported() bool {
	// Detection runs once — the terminal doesn't change mid-run — but the
	// answer isn't cached, so flipping the setting takes effect immediately
	// rather than at the next launch.
	kittyOnce.Do(func() {
		kittyDetected = kittyDetect()
		kittyTmux = os.Getenv("TMUX") != ""
	})

	// The environment wins over the setting, so either path can be tested
	// without editing config.
	switch os.Getenv("STREMIO_KITTY") {
	case "1":
		return true
	case "0":
		return false
	}

	switch store.CfgNow().KittyMode {
	case "kitty":
		return true
	case "default":
		return false
	}
	return kittyDetected
}

// kittyDetect reports whether the terminal around us — directly, or the
// outer client when nested in tmux — speaks kitty graphics.
func kittyDetect() bool {
	// Inside tmux the inner TERM/TERM_PROGRAM describe tmux itself (and
	// KITTY_WINDOW_ID can be left over from wherever the server started), so
	// ask it what the outer client speaks. If tmux can't say, fall through.
	// A terminal opened from a tmux shell inherits TMUX but names itself in
	// TERM_PROGRAM, so that case skips the query.
	tp := strings.ToLower(os.Getenv("TERM_PROGRAM"))
	if os.Getenv("TMUX") != "" && (tp == "" || tp == "tmux") && !overSSH() {
		if out, err := kittyTmuxClient(); err == nil && strings.Trim(out, ":") != "" {
			return kittyName(out)
		}
	}

	// TERM_PROGRAM before KITTY_WINDOW_ID: a terminal launched from kitty
	// inherits that variable and never clears it, so WezTerm started inside
	// kitty claims to be kitty. A terminal that names itself is the better
	// authority than one that merely inherited a variable.
	if tp != "" && tp != "tmux" {
		return kittyName(tp)
	}
	if os.Getenv("KITTY_WINDOW_ID") != "" {
		return true
	}

	// Over SSH the TERM we inherit describes the connection, not the
	// terminal drawing the screen: a forwarded "xterm-kitty" can sit in
	// front of a viewer with no graphics support. Distrust name-based
	// guesses there; STREMIO_KITTY=1 stays the explicit opt-in.
	if overSSH() {
		return false
	}
	return kittyName(strings.ToLower(os.Getenv("TERM")))
}

func overSSH() bool { return os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" }

// kittyName reports whether a terminal name is one that speaks kitty graphics.
func kittyName(s string) bool {
	return strings.Contains(s, "kitty") || strings.Contains(s, "ghostty")
}

// kittyTmuxClient asks tmux for the outer client's terminal name and type,
// e.g. "xterm-kitty:xterm-kitty".
func kittyTmuxClient() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "tmux", "display-message", "-p", "#{client_termname}:#{client_termtype}")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.ToLower(strings.TrimSpace(string(out))), nil
}

// kittyImageID hashes a poster URL to a nonzero 32-bit image id. Content
// addressed, so revisiting a title reuses (and replaces) the same image.
func kittyImageID(url string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(url))
	if id := h.Sum32(); id != 0 {
		return id
	}
	return 1
}

// kittyDims works out the placeholder rectangle in cells, preserving aspect.
// Terminal cells run roughly twice as tall as wide, so the same geometry as
// Size applies: a cols-wide block needs cols*ratio/2 rows.
func kittyDims(img image.Image, maxW, maxH int) (int, int) {
	b := img.Bounds()
	return Size(b.Dx(), b.Dy(), maxW, maxH)
}

// KittyBlock renders the cols×rows placeholder grid for an image id. The low
// 24 bits ride in a truecolor foreground, the high byte in a third diacritic,
// so the full 32-bit id survives; every cell carries all three diacritics
// rather than relying on the inherit-from-the-left rules, which break under
// horizontal scrolling and overlapping images. No lipgloss styling here —
// these lines must reach the screen byte-identical.
func KittyBlock(id uint32, cols, rows int) string {
	if cols <= 0 || rows <= 0 {
		return ""
	}
	lo := id & 0xFFFFFF
	msb := kittyDiacritic(int(id >> 24))
	fg := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", byte(lo>>16), byte(lo>>8), byte(lo))
	var sb strings.Builder
	for r := 0; r < rows; r++ {
		sb.WriteString(fg)
		rd := kittyDiacritic(r)
		for c := 0; c < cols; c++ {
			sb.WriteRune(kittyPlaceholder)
			sb.WriteRune(rd)
			sb.WriteRune(kittyDiacritic(c))
			sb.WriteRune(msb)
		}
		sb.WriteString("\x1b[39m")
		if r < rows-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// ── Raw channel ───────────────────────────────────────────────────────────────

// kittyMu serialises our own raw writes; each chunk goes out in a single
// Write syscall, and the APC framing keeps a chunk intact even if a renderer
// frame lands between two chunks — transmits and virtual placements draw
// nothing and touch no cursor state.
var kittyMu sync.Mutex

// kittyLive maps a transmitted id to the pixel width it was uploaded at, so
// quit can free them and a re-placement can tell whether the resident data
// still suits the new size. The id is a hash of the url alone, so the same
// poster at two sizes shares one — re-placing without checking would stretch
// a small upload across a large slot.
var kittyLive = map[uint32]int{}

// kittyOrder is the transmit order, oldest first, for eviction.
var kittyOrder []uint32

// kittyMaxLive caps how many images are left in the terminal.
//
// The terminal has its own quota — 320MB in kitty — and evicts on its own
// when it fills, preferring images with no placement, which is what ours
// become as soon as you move off them. That would leave kittyLive claiming
// an image is resident after the pixels are gone, and the placement sent for
// it would draw nothing.
//
// Staying far inside the quota keeps the two in step: at 512 pixels wide an
// RGBA poster is around 1.5MB, so 32 of them is about 48MB.
const kittyMaxLive = 32

// kittyRemember records a transmitted image, returning ids evicted to stay
// under the cap. Caller holds kittyMu and emits the deletes after unlocking.
// kittyTouch moves id to the back of the eviction order. Caller holds kittyMu.
func kittyTouch(id uint32) {
	for i, v := range kittyOrder {
		if v == id {
			kittyOrder = append(kittyOrder[:i], kittyOrder[i+1:]...)
			break
		}
	}
	kittyOrder = append(kittyOrder, id)
}

func kittyRemember(id uint32, px int) []uint32 {
	// Most recently used, so the image on screen is never the one evicted.
	kittyTouch(id)
	kittyLive[id] = px

	var drop []uint32
	for len(kittyOrder) > kittyMaxLive {
		old := kittyOrder[0]
		kittyOrder = kittyOrder[1:]
		if old == id {
			continue
		}
		delete(kittyLive, old)
		drop = append(drop, old)
	}
	return drop
}

func kittyEmit(payload []byte) {
	kittyMu.Lock()
	defer kittyMu.Unlock()
	// One Write per escape sequence; short enough to leave the runtime in a
	// single syscall.
	for len(payload) > 0 {
		end := bytes.Index(payload, []byte("\x1b\\"))
		end += 2
		kittyWriteOne(payload[:end])
		payload = payload[end:]
	}
}

// kittyWriteOne sends a single escape sequence. Inside tmux it rides DCS
// passthrough (ESC P tmux; … ESC \) with every inner ESC doubled, so tmux
// forwards the bytes to the outer terminal instead of swallowing them.
// Callers hold kittyMu.
func kittyWriteOne(seq []byte) {
	if kittyTmux {
		seq = kittyWrapTmux(seq)
	}
	os.Stdout.Write(seq)
}

// kittyWrapTmux encloses one escape sequence in tmux DCS passthrough, with
// every inner ESC doubled per the tmux passthrough protocol.
func kittyWrapTmux(seq []byte) []byte {
	var b []byte
	b = append(b, "\x1bPtmux;"...)
	b = append(b, bytes.ReplaceAll(seq, []byte{0x1b}, []byte{0x1b, 0x1b})...)
	return append(b, "\x1b\\"...)
}

// kittyAPC wraps control data and base64 payload in an APC escape.
func kittyAPC(ctrl string, payload []byte) []byte {
	var b []byte
	b = append(b, "\x1b_G"...)
	b = append(b, ctrl...)
	if len(payload) > 0 {
		b = append(b, ';')
		b = append(b, payload...)
	}
	return append(b, "\x1b\\"...)
}

// kittyPixelWidth is the width an image is uploaded at for a given placement.
// Pixels, not cells: enough that the terminal's downscale has something to
// work with, small enough to keep the upload quick.
func kittyPixelWidth(srcW, cols int) int {
	return min(srcW, max(192, min(cols*8, 512)))
}

// kittyTransmit encodes an image to chunked PNG upload escapes. Transmit
// only (a=t) — it stores the pixels invisibly and draws nothing; the
// companion kittyPlace binds the stored image to the placeholder cells.
func kittyTransmit(img image.Image, id uint32, cols int) []byte {
	pxW := kittyPixelWidth(img.Bounds().Dx(), cols)
	pxH := max(1, pxW*img.Bounds().Dy()/max(1, img.Bounds().Dx()))
	raw := encodePNG(boxScale(img, pxW, pxH))
	b64 := make([]byte, base64.StdEncoding.EncodedLen(len(raw)))
	base64.StdEncoding.Encode(b64, raw)

	const chunk = 4096 // spec ceiling; a multiple of 4 keeps base64 whole
	out := make([]byte, 0, len(b64)+(len(b64)/chunk+1)*16+64)
	first := true
	for len(b64) > 0 {
		n := min(len(b64), chunk)
		out = append(out, "\x1b_G"...)
		if first {
			out = fmt.Appendf(out, "a=t,t=d,f=100,i=%d,q=2,", id)
			first = false
		}
		if n == len(b64) {
			out = append(out, "m=0;"...)
		} else {
			out = append(out, "m=1;"...)
		}
		out = append(out, b64[:n]...)
		out = append(out, "\x1b\\"...)
		b64 = b64[n:]
	}
	return out
}

// encodePNG writes an opaque RGB PNG with every row Up-filtered at zlib's
// fastest level. image/png tries all five filters per row, which costs
// about 2.5x the time for a file under a tenth smaller.
func encodePNG(img *image.RGBA) []byte {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	var out bytes.Buffer
	out.Grow(w * h * 3 / 2)
	out.WriteString("\x89PNG\r\n\x1a\n")
	chunk := func(typ string, data []byte) {
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(len(data)))
		out.Write(n[:])
		out.WriteString(typ)
		out.Write(data)
		c := crc32.Update(crc32.ChecksumIEEE([]byte(typ)), crc32.IEEETable, data)
		binary.BigEndian.PutUint32(n[:], c)
		out.Write(n[:])
	}
	var hdr [13]byte
	binary.BigEndian.PutUint32(hdr[0:], uint32(w))
	binary.BigEndian.PutUint32(hdr[4:], uint32(h))
	hdr[8], hdr[9] = 8, 2 // 8-bit truecolour
	chunk("IHDR", hdr[:])

	var z bytes.Buffer
	zw, _ := zlib.NewWriterLevel(&z, zlib.BestSpeed)
	row := make([]byte, 1+w*3)
	prev := make([]byte, w*3)
	cur := make([]byte, w*3)
	row[0] = 2 // Up
	for y := range h {
		p := img.Pix[img.PixOffset(img.Bounds().Min.X, img.Bounds().Min.Y+y):]
		for x := range w {
			cur[x*3], cur[x*3+1], cur[x*3+2] = p[x*4], p[x*4+1], p[x*4+2]
		}
		for i := range cur {
			row[1+i] = cur[i] - prev[i]
		}
		zw.Write(row)
		prev, cur = cur, prev
	}
	zw.Close()
	chunk("IDAT", z.Bytes())
	chunk("IEND", nil)
	return out.Bytes()
}

// kittyPlace re-binds an already-transmitted image to a new rectangle — the
// cheap part of a resize, with no pixel upload.
func kittyPlace(id uint32, cols, rows int) []byte {
	return kittyAPC(fmt.Sprintf("a=p,U=1,i=%d,c=%d,r=%d,q=2", id, cols, rows), nil)
}

// kittyDelete drops an image's placements; data without placements is
// reclaimed under quota pressure.
func kittyDelete(id uint32) []byte {
	return kittyAPC(fmt.Sprintf("a=d,d=i,i=%d,q=2", id), nil)
}

// kittyDeleteData frees the image data as well, for quit-time cleanup.
func kittyDeleteData(id uint32) []byte {
	return kittyAPC(fmt.Sprintf("a=d,d=I,i=%d,q=2", id), nil)
}

// KittyCleanup frees every image transmitted this session. Called from
// Run's cleanup after the renderer has exited, so there is no frame to
// race with.
func KittyCleanup() {
	kittyMu.Lock()
	defer kittyMu.Unlock()
	for id := range kittyLive {
		kittyWriteOne(kittyDeleteData(id))
		delete(kittyLive, id)
	}
	kittyOrder = nil
}

// ── Commands ──────────────────────────────────────────────────────────────────

// KittyPosterMsg reports a finished transmit. pid guards against stale
// arrivals after the cursor has moved on.
type KittyPosterMsg struct {
	PID        AsyncID
	ID         uint32
	URL        string
	Cols, Rows int
	OK         bool
}

// KittyLoadCmd fetches the poster, uploads it over the raw channel, and
// reports back. Runs in a background goroutine — safe like Fetch.
func KittyLoadCmd(pid AsyncID, url string, maxW, maxH int) tea.Cmd {
	return func() tea.Msg {
		fail := func() tea.Msg {
			return KittyPosterMsg{PID: pid, OK: false}
		}
		if url == "" {
			return fail()
		}
		// A known image that the terminal still holds at the right size
		// needs no bytes at all — not a download, not a decode, just a
		// placement. Scrolling back through a list hits this.
		//
		// Only the wanted variant qualifies. Checking the fallbacks too
		// would keep serving the medium one after the quality setting was
		// raised, because that is what happens to still be resident.
		if variants := Variants(url, true); len(variants) > 0 {
			u := variants[0]
			if d, known := CacheDims.Get(u); known {
				if cols, rows := Size(d.W, d.H, maxW, maxH); cols > 0 && rows > 0 {
					id := kittyImageID(u)

					kittyMu.Lock()
					resident := kittyLive[id] == kittyPixelWidth(d.W, cols)
					if resident {
						kittyTouch(id) // being shown counts as use
					}
					kittyMu.Unlock()

					if resident {
						kittyEmit(kittyPlace(id, cols, rows))
						return KittyPosterMsg{
							PID: pid, ID: id, URL: u,
							Cols: cols, Rows: rows, OK: true,
						}
					}
				}
			}
		}

		// Largest first. The URL that actually loaded is what gets reported
		// back, since the resize path looks the dimensions up by it.
		var img image.Image
		var ok bool
		for _, u := range Variants(url, true) {
			if img, ok = posterImg(u); ok {
				url = u
				break
			}
		}
		if !ok {
			return fail()
		}
		cols, rows := kittyDims(img, maxW, maxH)
		if cols <= 0 || rows <= 0 {
			return fail()
		}
		id := kittyImageID(url)
		px := kittyPixelWidth(img.Bounds().Dx(), cols)

		raw := kittyTransmit(img, id, cols)
		if len(raw) == 0 {
			return fail()
		}
		// Transmit first, then bind: the pixels must exist before the
		// virtual placement references them, and both must complete before
		// any placeholder cell reaches the View (see KittyPosterMsg).
		raw = append(raw, kittyPlace(id, cols, rows)...)
		kittyEmit(raw)

		kittyMu.Lock()
		drop := kittyRemember(id, px)
		kittyMu.Unlock()

		// Data and placements both: an evicted image is one we have stopped
		// tracking, so leaving its pixels behind is a leak in the terminal.
		for _, old := range drop {
			kittyEmit(kittyDeleteData(old))
		}
		return KittyPosterMsg{PID: pid, ID: id, URL: url, Cols: cols, Rows: rows, OK: true}
	}
}

// KittyDeleteCmd drops a superseded image without blocking the UI.
func KittyDeleteCmd(id uint32) tea.Cmd {
	return func() tea.Msg {
		// Placement only (d=i) — the pixels stay in the terminal, so the
		// id stays in kittyLive and coming back needs a placement rather
		// than another upload. Scrolling an episode list otherwise
		// re-sends a few hundred kilobytes per keypress.
		kittyEmit(kittyDelete(id))
		return nil
	}
}

// KittyPlaceCmd re-binds a live image to a resized rectangle.
func KittyPlaceCmd(id uint32, cols, rows int) tea.Cmd {
	return func() tea.Msg {
		kittyEmit(kittyPlace(id, cols, rows))
		return nil
	}
}
