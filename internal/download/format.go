package download

import (
	"fmt"
	"strings"
)

// ── Formatting ────────────────────────────────────────────────────────────────

func FmtBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit && exp < 3; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}

func FmtETA(d Download) string {
	if d.Speed <= 0 || d.Total <= 0 || d.Done >= d.Total {
		return ""
	}
	secs := float64(d.Total-d.Done) / float64(d.Speed)
	return FmtSecs(secs) + " left"
}

func FmtSecs(s float64) string {
	if s < 0 {
		s = 0
	}
	t := int(s)
	h, m, sec := t/3600, (t%3600)/60, t%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, sec)
	}
	return fmt.Sprintf("%02d:%02d", m, sec)
}

// Paths in a torrent are arbitrarily deep — typically
// "<torrent name>/Season 01/Family Guy - S01E01.mp4". Treating only the first
// segment as "the folder" grouped everything under the torrent name, which is
// already the breadcrumb, and left "Season 01/" glued to every filename.

func BaseName(p string) string {
	p = strings.TrimSuffix(p, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
