package stremio

import (
	"reflect"
	"strings"
)

// CleanText drops what addon text mustn't send to the terminal: control
// characters, which include the start of an escape sequence (clearing the
// screen, retitling the window, writing the clipboard), and the bidi
// overrides that make a terminal draw the rest of the line backwards.
// Newlines stay, tabs become spaces.
//
// Joined emoji (a family, a flag with a rainbow) are split into their parts,
// and skin tones dropped: terminals that don't join them draw each part, so
// the line came out wider than it was measured and wrapped. A joiner after
// a letter is kept, as scripts like Hindi need it.
func CleanText(s string) string {
	if !needsCleaning(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	prev := rune(0) // the last one kept
	for _, r := range s {
		switch {
		case r == '\t':
			r = ' '
		case r == '\n':
		case r < 0x20, r >= 0x7f && r <= 0x9f, isBidiControl(r), isSkinTone(r):
			continue
		case r == zwj && prev >= emojiStart:
			continue
		}
		b.WriteRune(r)
		prev = r
	}
	return b.String()
}

const (
	zwj        = 0x200d
	emojiStart = 0x2190 // arrows and symbols onward; letters are all below
)

func needsCleaning(s string) bool {
	for _, r := range s {
		if r < 0x20 && r != '\n' || r >= 0x7f && r <= 0x9f || isBidiControl(r) || isSkinTone(r) || r == zwj {
			return true
		}
	}
	return false
}

func isSkinTone(r rune) bool { return r >= 0x1f3fb && r <= 0x1f3ff }

func isBidiControl(r rune) bool {
	return r >= 0x202a && r <= 0x202e || r >= 0x2066 && r <= 0x2069 || r == 0x200e || r == 0x200f || r == 0x061c
}

// cleanStrings runs CleanText over every string reachable from v, so nothing
// an addon sends reaches the screen unchecked.
func cleanStrings(v reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			cleanStrings(v.Elem())
		}
	case reflect.String:
		if v.CanSet() {
			if s := v.String(); needsCleaning(s) {
				v.SetString(CleanText(s))
			}
		}
	case reflect.Struct:
		for i := range v.NumField() {
			cleanStrings(v.Field(i))
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			cleanStrings(v.Index(i))
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			e := reflect.New(iter.Value().Type()).Elem()
			e.Set(iter.Value())
			cleanStrings(e)
			v.SetMapIndex(iter.Key(), e)
		}
	}
}
