package poster

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image/png"
	"os"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"stremio-cliuwu/internal/store"
)

// kittyOut captures what the raw channel writes, with the image tracking
// reset.
func kittyOut(t *testing.T) func() string {
	f, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = f
	kittyLive, kittyOrder = map[uint32]int{}, nil
	t.Cleanup(func() {
		os.Stdout = saved
		f.Close()
		kittyLive, kittyOrder, kittyTmux = map[uint32]int{}, nil, false
	})
	var read int64
	return func() string {
		b, _ := os.ReadFile(f.Name())
		out := string(b[read:])
		read = int64(len(b))
		return out
	}
}

var apc = regexp.MustCompile("\x1b_G([^;\x1b]*)(?:;([^\x1b]*))?\x1b\\\\")

func TestKittyLoadTransmitsThenPlaces(t *testing.T) {
	asked := usePosters(t, map[string][]byte{"/poster/large/tt1/img": encode(t, halves(300, 450), false)})
	out := kittyOut(t)
	url := "https://images.metahub.space/poster/medium/tt1/img"

	msg := KittyLoadCmd(7, url, 20, 40)().(KittyPosterMsg)
	if !msg.OK || msg.PID != 7 || msg.Cols != 20 || msg.Rows != 15 || !strings.Contains(msg.URL, "/large/") {
		t.Fatalf("got %+v", msg)
	}
	seqs := apc.FindAllStringSubmatch(out(), -1)
	if len(seqs) < 2 {
		t.Fatalf("%d escapes", len(seqs))
	}
	// The upload: chunks of at most 4096, the first carrying the keys, the
	// last m=0, and together a PNG at the upload width.
	if want := fmt.Sprintf("a=t,t=d,f=100,i=%d,q=2,", msg.ID); !strings.HasPrefix(seqs[0][1], want) {
		t.Fatalf("first chunk %q, want %q", seqs[0][1], want)
	}
	var b64 strings.Builder
	last := len(seqs) - 2
	for i, s := range seqs[:last+1] {
		if len(s[2]) > 4096 {
			t.Fatalf("chunk %d is %d bytes", i, len(s[2]))
		}
		if more := strings.HasSuffix(s[1], "m=1"); more != (i < last) {
			t.Fatalf("chunk %d: %q", i, s[1])
		}
		b64.WriteString(s[2])
	}
	raw, err := base64.StdEncoding.DecodeString(b64.String())
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if w := img.Bounds().Dx(); w != kittyPixelWidth(300, 20) {
		t.Fatalf("uploaded %dpx wide, want %d", w, kittyPixelWidth(300, 20))
	}
	if place := seqs[len(seqs)-1][1]; place != fmt.Sprintf("a=p,U=1,i=%d,c=20,r=15,q=2", msg.ID) {
		t.Fatalf("placement %q", place)
	}

	// Again at the same size: a placement, nothing downloaded or uploaded.
	n := asked.Load()
	again := KittyLoadCmd(8, url, 20, 40)().(KittyPosterMsg)
	if got := out(); !again.OK || asked.Load() != n || strings.Contains(got, "a=t") || !strings.Contains(got, "a=p") {
		t.Fatalf("revisit re-sent: asked %d→%d, %q", n, asked.Load(), got)
	}
	// Much bigger: the resident pixels are too small, so it uploads again.
	KittyLoadCmd(9, url, 60, 90)()
	if !strings.Contains(out(), "a=t") {
		t.Fatal("a larger slot reused the small upload")
	}

	KittyCleanup()
	if got := out(); !strings.Contains(got, fmt.Sprintf("a=d,d=I,i=%d", msg.ID)) || len(kittyLive) != 0 {
		t.Fatalf("cleanup: %q", got)
	}
}

func TestKittyEvictsOldest(t *testing.T) {
	files := map[string][]byte{}
	png := encode(t, halves(20, 30), false)
	for i := range kittyMaxLive + 1 {
		files[fmt.Sprintf("/p%d.png", i)] = png
	}
	usePosters(t, files)
	out := kittyOut(t)
	for i := range kittyMaxLive + 1 {
		if !KittyLoadCmd(1, fmt.Sprintf("https://x.test/p%d.png", i), 10, 20)().(KittyPosterMsg).OK {
			t.Fatalf("poster %d failed", i)
		}
	}
	first := kittyImageID("https://x.test/p0.png")
	if !strings.Contains(out(), fmt.Sprintf("a=d,d=I,i=%d,", first)) || len(kittyLive) != kittyMaxLive {
		t.Fatalf("oldest not freed: %d live", len(kittyLive))
	}
	if KittyLoadCmd(1, "https://x.test/missing.png", 10, 20)().(KittyPosterMsg).OK {
		t.Fatal("a missing poster reported OK")
	}
}

func TestKittyTmuxPassthrough(t *testing.T) {
	out := kittyOut(t)
	kittyTmux = true
	KittyPlaceCmd(5, 2, 3)()
	KittyDeleteCmd(5)()
	got := out()
	want := "\x1bPtmux;\x1b\x1b_Ga=p,U=1,i=5,c=2,r=3,q=2\x1b\x1b\\\x1b\\" +
		"\x1bPtmux;\x1b\x1b_Ga=d,d=i,i=5,q=2\x1b\x1b\\\x1b\\"
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestKittyBlock(t *testing.T) {
	id := uint32(0x05_A1B2C3)
	block := KittyBlock(id, 3, 2)
	rows := strings.Split(block, "\n")
	if len(rows) != 2 {
		t.Fatalf("%d rows", len(rows))
	}
	for r, row := range rows {
		if !strings.HasPrefix(row, "\x1b[38;2;161;178;195m") || !strings.HasSuffix(row, "\x1b[39m") {
			t.Fatalf("row %d colour: %q", r, row)
		}
		cells := strings.TrimSuffix(strings.TrimPrefix(row, "\x1b[38;2;161;178;195m"), "\x1b[39m")
		if n := utf8.RuneCountInString(cells); n != 3*4 {
			t.Fatalf("row %d: %d runes, want 3 cells of 4", r, n)
		}
		rs := []rune(cells)
		for c := range 3 {
			cell := rs[c*4 : c*4+4]
			if cell[0] != kittyPlaceholder || cell[1] != kittyDiacritic(r) || cell[2] != kittyDiacritic(c) || cell[3] != kittyDiacritic(5) {
				t.Fatalf("cell %d,%d: %q", r, c, string(cell))
			}
		}
	}
	if KittyBlock(id, 0, 2) != "" || kittyDiacritic(-1) != kittyDiacritic(0) || kittyDiacritic(1<<20) != kittyDiacritic(0) {
		t.Fatal("edges")
	}
	if kittyImageID("a") == kittyImageID("b") || kittyImageID("a") == 0 {
		t.Fatal("ids")
	}
}

func TestKittySupportedOverrides(t *testing.T) {
	t.Setenv("STREMIO_KITTY", "1")
	if !KittySupported() {
		t.Fatal("STREMIO_KITTY=1")
	}
	t.Setenv("STREMIO_KITTY", "0")
	if KittySupported() {
		t.Fatal("STREMIO_KITTY=0")
	}
	t.Setenv("STREMIO_KITTY", "")
	usePosters(t, nil)
	for mode, want := range map[string]bool{"kitty": true, "default": false} {
		cfg := store.CfgNow()
		cfg.KittyMode = mode
		store.UseConfig(cfg)
		if KittySupported() != want {
			t.Fatalf("mode %q", mode)
		}
	}
}
