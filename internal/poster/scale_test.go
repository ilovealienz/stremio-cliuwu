package poster

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"math"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	r := rand.New(rand.NewSource(1))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = uint8(r.Intn(256)), uint8(r.Intn(32)), uint8(i/4%256), 255
	}
	return img
}

// The lookup tables must give what math.Pow per pixel gave.
func TestBoxScaleMatchesPow(t *testing.T) {
	src := testImage(300, 450)
	for _, w := range []int{300, 128, 37} {
		h := w * 450 / 300
		got := boxScale(src, w, h)
		for y := range h {
			y0, y1 := y*450/h, max((y+1)*450/h, y*450/h+1)
			for x := range w {
				x0, x1 := x*300/w, max((x+1)*300/w, x*300/w+1)
				for c := range 3 {
					var sum float64
					for sy := y0; sy < y1; sy++ {
						for sx := x0; sx < x1; sx++ {
							sum += math.Pow(float64(src.Pix[src.PixOffset(sx, sy)+c])/255, 2.2)
						}
					}
					want := int(math.Pow(sum/float64((y1-y0)*(x1-x0)), 1/2.2)*255 + 0.5)
					d := int(got.Pix[got.PixOffset(x, y)+c]) - want
					if d < -1 || d > 1 {
						t.Fatalf("w=%d (%d,%d) c%d: got %d want %d", w, x, y, c, got.Pix[got.PixOffset(x, y)+c], want)
					}
				}
			}
		}
	}
}

func TestEncodePNGRoundTrip(t *testing.T) {
	src := testImage(64, 40)
	sub := src.SubImage(image.Rect(5, 7, 50, 33)).(*image.RGBA)
	for _, img := range []*image.RGBA{src, sub} {
		back, err := png.Decode(bytes.NewReader(encodePNG(img)))
		if err != nil {
			t.Fatal(err)
		}
		b := img.Bounds()
		if back.Bounds().Dx() != b.Dx() || back.Bounds().Dy() != b.Dy() {
			t.Fatalf("size %v, want %v", back.Bounds(), b)
		}
		for y := range b.Dy() {
			for x := range b.Dx() {
				r, g, bl, a := back.At(x, y).RGBA()
				c := img.RGBAAt(b.Min.X+x, b.Min.Y+y)
				if uint8(r>>8) != c.R || uint8(g>>8) != c.G || uint8(bl>>8) != c.B || a != 0xffff {
					t.Fatalf("pixel %d,%d differs", x, y)
				}
			}
		}
	}
}

func TestPosterLimits(t *testing.T) {
	big := image.NewRGBA(image.Rect(0, 0, 1, 1))
	var b bytes.Buffer
	png.Encode(&b, big)
	raw := b.Bytes()
	// Claim 30000x30000 in the header.
	raw[16], raw[17], raw[18], raw[19] = 0, 0, 0x75, 0x30
	raw[20], raw[21], raw[22], raw[23] = 0, 0, 0x75, 0x30
	binary.BigEndian.PutUint32(raw[29:], crc32.ChecksumIEEE(raw[12:29]))
	if c, _, err := image.DecodeConfig(bytes.NewReader(raw)); err != nil || c.Width != 30000 {
		t.Fatalf("test image header: %v %v", c, err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/huge" {
			w.Write(make([]byte, maxPosterBytes+10))
			return
		}
		w.Write(raw)
	}))
	defer srv.Close()

	if _, ok := posterBytes(srv.URL + "/huge"); ok {
		t.Error("cut-off file accepted")
	}
	if _, ok := posterImg(srv.URL + "/bomb"); ok {
		t.Error("30000x30000 image decoded")
	}
}

// Scaling straight from YCbCr must match converting to RGBA first.
func TestBoxScaleYCbCr(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	for _, ratio := range []image.YCbCrSubsampleRatio{image.YCbCrSubsampleRatio420, image.YCbCrSubsampleRatio422, image.YCbCrSubsampleRatio444, image.YCbCrSubsampleRatio411, image.YCbCrSubsampleRatio440, image.YCbCrSubsampleRatio410} {
		full := image.NewYCbCr(image.Rect(0, 0, 101, 77), ratio)
		for _, p := range [][]byte{full.Y, full.Cb, full.Cr} {
			r.Read(p)
		}
		for _, img := range []*image.YCbCr{full, full.SubImage(image.Rect(3, 5, 96, 70)).(*image.YCbCr)} {
			rgba := toRGBA(img)
			for _, w := range []int{img.Bounds().Dx(), 40, 7} {
				h := w * img.Bounds().Dy() / img.Bounds().Dx()
				a, b := boxScale(rgba, w, h), boxScale(img, w, h)
				for i := range a.Pix {
					if d := int(a.Pix[i]) - int(b.Pix[i]); d < -1 || d > 1 {
						t.Fatalf("ratio %v bounds %v w=%d: byte %d is %d, want %d", ratio, img.Bounds(), w, i, b.Pix[i], a.Pix[i])
					}
				}
			}
		}
	}
}

// toSRGB must give the same byte as rounding in sRGB, including right at the
// edges between bytes where float rounding is most likely to slip.
func TestToSRGBEdges(t *testing.T) {
	gammaTables()
	want := func(v float32) int {
		b := 0
		for b < 255 && thrTab[b+1] <= v {
			b++
		}
		return b
	}
	for b := 1; b < 256; b++ {
		e := thrTab[b]
		for _, v := range []float32{e, math.Nextafter32(e, 0), math.Nextafter32(e, 2)} {
			if got := toSRGB(v); int(got) != want(v) {
				t.Fatalf("toSRGB(%g) = %d, want %d", v, got, want(v))
			}
		}
	}
	for k := 0; k <= 200000; k++ {
		v := float32(k) / 200000
		if got := toSRGB(v); int(got) != want(v) {
			t.Fatalf("toSRGB(%g) = %d, want %d", v, got, want(v))
		}
	}
}
