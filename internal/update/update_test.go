package update

import (
	"fmt"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	var out []string
	for _, c := range [][2]string{
		{"", "v0.6.7"}, {"dev", "v0.6.7"},
		{"0.6.7", "v0.6.7"}, {"0.6.7.1", "v0.6.7"}, {"0.6.7", "v0.6.7.1"},
		{"0.6.7-3-gabc1234", "v0.6.7"}, {"0.6.7-dirty", "v0.6.7"}, {"v0.6.6-1-gabc", "v0.6.7"},
		{"0.9.0", "v0.10.0"}, {"0.10.0", "v0.9.0"}, {"1.0", " v1.0.0 "},
	} {
		out = append(out, fmt.Sprintf("compare(%q,%q)=%d", c[0], c[1], Compare(c[0], c[1])))
	}
	golden(t, "compare_versions", out)
}
