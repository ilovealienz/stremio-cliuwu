package stremio

import (
	"reflect"
	"testing"
)

func TestCleanText(t *testing.T) {
	for in, want := range map[string]string{
		"Evil\x1b]52;c;aGk=\x07Film": "Evil]52;c;aGk=Film",
		"Clear\x1b[2Jme":             "Clear[2Jme",
		"\u202eexe.TXT":              "exe.TXT",
		"a\tb\nc\r":                  "a b\nc",
		"\u0085C1":                   "C1",
		"👨\u200d👩\u200d👧 x":          "👨👩👧 x",
		"👍🏽 ok":                      "👍 ok",
		"क्\u200dष":                  "क्\u200dष", // a joiner in Devanagari stays
		"進撃の巨人 🇯🇵 ⚙️ Pokémon": "進撃の巨人 🇯🇵 ⚙️ Pokémon",
	} {
		if got := CleanText(in); got != want {
			t.Errorf("CleanText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCleanStringsReachesEverything(t *testing.T) {
	type inner struct{ S string }
	v := struct {
		Name string
		Ptr  *inner
		List []inner
		Map  map[string]string
		Deep map[string]inner
		Any  any
		priv string
		Flex FlexString
	}{
		Name: "a\x1bb", Ptr: &inner{"c\x07d"}, List: []inner{{"e\x1bf"}},
		Map: map[string]string{"k": "g\x1bh"}, Deep: map[string]inner{"k": {"i\x1bj"}},
		Any: "untouched", priv: "p\x1bq", Flex: "r\x1bs",
	}
	cleanStrings(reflect.ValueOf(&v))
	if v.Name != "ab" || v.Ptr.S != "cd" || v.List[0].S != "ef" || v.Map["k"] != "gh" || v.Deep["k"].S != "ij" || v.Flex != "rs" {
		t.Fatalf("missed one: %+v %+v", v, *v.Ptr)
	}
	if v.priv != "p\x1bq" {
		t.Fatal("touched an unexported field")
	}
}
