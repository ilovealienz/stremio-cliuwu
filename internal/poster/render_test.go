package poster

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"stremio-cliuwu/internal/store"
	"stremio-cliuwu/internal/stremio"
)

// halves is a w×h image, red on top and blue underneath.
func halves(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		c := color.RGBA{255, 0, 0, 255}
		if y >= h/2 {
			c = color.RGBA{0, 0, 255, 255}
		}
		for x := range w {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

type posterHost struct {
	files map[string][]byte
	asked *atomic.Int32
}

func (p posterHost) RoundTrip(r *http.Request) (*http.Response, error) {
	p.asked.Add(1)
	rec := httptest.NewRecorder()
	if b, ok := p.files[r.URL.Path]; ok {
		rec.Write(b)
	} else {
		rec.WriteHeader(http.StatusNotFound)
	}
	return rec.Result(), nil
}

func usePosters(t *testing.T, files map[string][]byte) *atomic.Int32 {
	asked := new(atomic.Int32)
	saved, savedCfg, savedProfile := stremio.HTTPClient.Transport, store.CfgNow(), lipgloss.ColorProfile()
	stremio.HTTPClient.Transport = posterHost{files, asked}
	cfg := savedCfg
	cfg.Posters = true
	store.UseConfig(cfg)
	lipgloss.SetColorProfile(termenv.TrueColor)
	cachePosterBytes.Clear()
	cachePosterArt.Clear()
	CacheDims.Clear()
	t.Cleanup(func() {
		stremio.HTTPClient.Transport = saved
		store.UseConfig(savedCfg)
		lipgloss.SetColorProfile(savedProfile)
	})
	return asked
}

func encode(t *testing.T, img image.Image, asJPEG bool) []byte {
	var b bytes.Buffer
	var err error
	if asJPEG {
		err = jpeg.Encode(&b, img, &jpeg.Options{Quality: 95})
	} else {
		err = png.Encode(&b, img)
	}
	if err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestFetchRendersHalfBlocks(t *testing.T) {
	for _, asJPEG := range []bool{false, true} {
		name := "/poster/small/tt1/img"
		usePosters(t, map[string][]byte{name: encode(t, halves(40, 60), asJPEG)})
		url := "https://images.metahub.space/poster/medium/tt1/img"
		art := Fetch(url, 10, 20)
		rows := strings.Split(art, "\n")
		// 40×60 at ten cells wide is 10×15 pixels, two to a cell: 7 rows.
		if len(rows) != 7 {
			t.Fatalf("jpeg=%v: %d rows, want 7", asJPEG, len(rows))
		}
		for i, row := range rows {
			if n := strings.Count(row, "▀"); n != 10 {
				t.Fatalf("jpeg=%v row %d: %d cells, want 10", asJPEG, i, n)
			}
			if !strings.HasSuffix(row, "\x1b[0m") {
				t.Fatalf("row %d doesn't reset its colour", i)
			}
		}
		if !strings.Contains(rows[0], "38;2;25") || !strings.Contains(rows[6], ";48;2;0;0;25") {
			t.Fatalf("jpeg=%v: red top, blue bottom expected:\n%q\n%q", asJPEG, rows[0][:40], rows[6][:40])
		}
	}
}

func TestFetchCachesAndFallsBack(t *testing.T) {
	asked := usePosters(t, map[string][]byte{"/poster/large/tt2/img": encode(t, halves(20, 30), false)})
	url := "https://images.metahub.space/poster/medium/tt2/img"
	if Fetch(url, 10, 20) == "" {
		t.Fatal("small and medium are missing, large should have been used")
	}
	n := asked.Load()
	if n != 3 {
		t.Fatalf("asked %d times, want small, medium, then large", n)
	}
	Fetch(url, 10, 20)
	if asked.Load() != n {
		t.Fatal("the rendered poster wasn't cached")
	}
	if Fetch("https://images.metahub.space/poster/medium/missing/img", 10, 20) != "" {
		t.Fatal("art for a poster that isn't there")
	}
}

func TestFetchRefusesBadImages(t *testing.T) {
	// A tiny PNG claiming 20000×20000 pixels: refused before decoding.
	var huge bytes.Buffer
	png.Encode(&huge, image.NewGray(image.Rect(0, 0, 1, 1)))
	b := huge.Bytes()
	b[16], b[17], b[18], b[19] = 0, 0, 0x4e, 0x20 // width in IHDR
	b[20], b[21], b[22], b[23] = 0, 0, 0x4e, 0x20 // height
	usePosters(t, map[string][]byte{"/huge.png": b, "/junk.png": []byte("not an image")})
	for _, u := range []string{"https://x.test/huge.png", "https://x.test/junk.png"} {
		if Fetch(u, 10, 20) != "" {
			t.Fatalf("%s rendered", u)
		}
	}
	// Posters off: nothing is fetched at all.
	cfg := store.CfgNow()
	cfg.Posters = false
	store.UseConfig(cfg)
	if Fetch("https://x.test/huge.png?off", 10, 20) != "" {
		t.Fatal("rendered with posters off")
	}
}

func TestRender256Colours(t *testing.T) {
	usePosters(t, nil)
	lipgloss.SetColorProfile(termenv.ANSI256)
	art := renderPoster(halves(4, 8), 4, 8)
	if !strings.Contains(art, "\x1b[38;5;196;48;5;196m▀") || !strings.Contains(art, "\x1b[38;5;21;48;5;21m▀") {
		t.Fatalf("256-colour red and blue expected: %q", art)
	}
	lipgloss.SetColorProfile(termenv.Ascii)
	if renderPoster(halves(4, 8), 4, 8) != "" {
		t.Fatal("drew a poster with no colour")
	}
	if rgb256(128, 128, 128) < 232 || rgb256(0, 0, 0) != 16 || rgb256(255, 255, 255) != 231 {
		t.Fatal("greys should use the grey ramp")
	}
}

func TestSize(t *testing.T) {
	for _, c := range []struct{ sw, sh, mw, mh, w, h int }{
		{400, 600, 20, 40, 20, 15}, // width-bound
		{400, 600, 40, 10, 13, 10}, // height-bound
		{600, 100, 20, 40, 20, 1},  // never zero rows
		{0, 600, 20, 40, 0, 0},
	} {
		if w, h := Size(c.sw, c.sh, c.mw, c.mh); w != c.w || h != c.h {
			t.Errorf("Size(%d,%d,%d,%d) = %d,%d want %d,%d", c.sw, c.sh, c.mw, c.mh, w, h, c.w, c.h)
		}
	}
}
