package store

import ()

// ── Completion thresholds ─────────────────────────────────────────────────────
//
// A flat percentage treats every runtime the same, which breaks badly at the
// extremes: 70% of a two and a half hour film leaves 45 minutes unwatched, and
// 5% of a ten minute cartoon is half a minute.
//
// The approach the established players take is a percentage bounded by
// absolute limits — Kodi pairs playcountminimumpercent with a flat
// ignoresecondsatstart of 180, Jellyfin pairs MinResumePct with
// MinResumeDurationSeconds. Same idea here.
//
// Deliberately generous at the end: film credits regularly run ten minutes,
// and stopping when they start should count as having watched the thing.

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// watchedAllowance is how much runtime may remain while still counting as
// watched — 14%, bounded to 25s…18min. That puts most things at 86% and a
// feature-length film nearer 88%, since the cap only starts to bite past
// about two hours.
func watchedAllowance(dur float64) float64 {
	return clampF(0.14*dur, 25, 18*60)
}

// resumeFloor is how far in you must be before a resume point is worth
// keeping — 5%, bounded to 15s…3min. Below it, you've barely started.
func resumeFloor(dur float64) float64 {
	return clampF(0.05*dur, 15, 3*60)
}

// PrefetchLead is how long before the end to start resolving the next episode.
// Enough time for a debrid addon to answer, without jumping the gun on a film.
// Never before the file counts as watched, so a stream that drops first
// doesn't leave you on the next episode's list.
func PrefetchLead(dur float64) float64 {
	return min(clampF(0.15*dur, 90, 6*60), watchedAllowance(dur))
}

// IsWatchedAt reports whether a position counts as having finished the video.
func IsWatchedAt(pos, dur float64) bool {
	if dur <= 0 {
		return false
	}
	return dur-pos <= watchedAllowance(dur)
}

// HasResumePoint reports whether a position is worth resuming from: far enough
// in to be meaningful, not so far in that it's effectively finished.
func HasResumePoint(pos, dur float64) bool {
	if dur <= 0 || pos <= 0 {
		return false
	}
	return pos >= resumeFloor(dur) && !IsWatchedAt(pos, dur)
}
