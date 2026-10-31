package stremio

import "testing"

func TestEpQueueStep(t *testing.T) {
	q := &EpQueue{
		Season: 1,
		Episodes: []Video{
			{ID: "s1e1", Season: 1, Episode: 1, Released: "2001-01-01"},
			{ID: "s1e2", Season: 1, Episode: 2, Released: "2001-01-08"},
			{ID: "s1e3", Season: 1, Episode: 3, Released: "2999-01-01"},
		},
		All: []Video{{ID: "s2e1", Season: 2, Episode: 1, Released: "2002-01-01"}},
	}
	at := func(i int) *EpQueue { c := *q; c.Index = i; return &c }

	for _, c := range []struct {
		from, delta int
		want        string // "" for no step
	}{
		{0, 1, "s1e2"},
		{1, -1, "s1e1"},
		{0, -1, ""}, // start of season
		{1, 1, ""},  // next one hasn't aired
		{2, -1, "s1e2"},
		{2, 1, ""}, // end of season, even with season 2 in All
	} {
		nq, ok := at(c.from).Step(c.delta)
		got := ""
		if ok {
			got = nq.Episodes[nq.Index].ID
		}
		if got != c.want {
			t.Errorf("Step(%d) from %d = %q, want %q", c.delta, c.from, got, c.want)
		}
	}
	if q.Index != 0 {
		t.Fatal("Step changed the original queue")
	}
	var nilQ *EpQueue
	if _, ok := nilQ.Step(1); ok || nilQ.HasNext() || nilQ.HasPrev() {
		t.Fatal("nil queue should not step")
	}
	if v, _, ok := at(2).Upcoming(); !ok || v.ID != "s2e1" {
		t.Fatal("Upcoming should still name next season's first episode")
	}
}

func TestEpQueueNextSeason(t *testing.T) {
	aired, later := "2001-01-01", "2999-01-01"
	all := []Video{
		{ID: "sp1", Season: 0, Episode: 1, Released: aired},
		{ID: "s1e1", Season: 1, Episode: 1, Released: aired},
		{ID: "s1e2", Season: 1, Episode: 2, Released: aired},
		{ID: "s3e2", Season: 3, Episode: 2, Released: aired},
		{ID: "s3e1", Season: 3, Episode: 1, Released: aired},
		{ID: "s3e1dup", Season: 3, Episode: 1, Released: aired},
		{ID: "s4e1", Season: 4, Episode: 1, Released: later},
	}
	season := func(s, i int) *EpQueue {
		var eps []Video
		for _, v := range all {
			if v.Season == s && v.ID != "s3e1" {
				eps = append(eps, v)
			}
		}
		return &EpQueue{Season: s, Episodes: eps, Index: i, All: all}
	}
	for _, c := range []struct {
		name  string
		q     *EpQueue
		cross bool
		want  string
	}{
		{"mid season", season(1, 0), true, "s1e2"},
		{"finale, off", season(1, 1), false, ""},
		{"finale, on", season(1, 1), true, "s3e1dup"}, // season 2 missing, duplicate dropped
		{"premiere not out", season(3, 1), true, ""},
		{"from specials", season(0, 0), true, ""},
		{"nil", nil, true, ""},
	} {
		nq, ok := c.q.Next(c.cross)
		got := ""
		if ok {
			got = nq.Episodes[nq.Index].ID
		}
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
		if c.name == "finale, on" && ok && (nq.Season != 3 || len(nq.Episodes) != 2 || nq.Episodes[1].ID != "s3e2" || len(nq.All) != len(all)) {
			t.Errorf("next season queue: %+v", nq)
		}
	}
}
