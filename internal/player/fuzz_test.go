package player

import (
	"strings"
	"testing"
)

// The subtitle setting is typed by hand and handed to mpv as --slang.
func FuzzLangList(f *testing.F) {
	for _, s := range []string{"eng, jpn", " , ,", "en,,ja ", "", "\teng\n"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		l := langList(s)
		if langList(l) != l {
			t.Fatalf("langList(%q) = %q, then %q", s, l, langList(l))
		}
		if l == "" {
			return
		}
		for _, f := range strings.Split(l, ",") {
			if f == "" || strings.TrimSpace(f) != f {
				t.Fatalf("langList(%q) = %q", s, l)
			}
		}
	})
}
