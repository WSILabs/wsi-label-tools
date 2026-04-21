package tifflow

import (
	"errors"
	"testing"
)

func TestRangeMap_NoOverlap(t *testing.T) {
	m := &RangeMap{}
	if err := m.Add(Range{Start: 0, End: 10, Owner: 0, What: "ifd"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Add(Range{Start: 10, End: 20, Owner: 0, What: "data"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Add(Range{Start: 20, End: 30, Owner: 1, What: "ifd"}); err != nil {
		t.Fatal(err)
	}
}

func TestRangeMap_Overlap(t *testing.T) {
	m := &RangeMap{}
	_ = m.Add(Range{Start: 0, End: 20})
	err := m.Add(Range{Start: 10, End: 30})
	if !errors.Is(err, ErrOverlap) {
		t.Errorf("err = %v, want ErrOverlap", err)
	}
}

func TestRangeMap_MinOffsetOf(t *testing.T) {
	m := &RangeMap{}
	_ = m.Add(Range{Start: 100, End: 110, Owner: 0})
	_ = m.Add(Range{Start: 50, End: 60, Owner: 1})
	_ = m.Add(Range{Start: 200, End: 210, Owner: 1})
	if got := m.MinOffsetOfOwner(1); got != 50 {
		t.Errorf("MinOffsetOfOwner(1) = %d, want 50", got)
	}
}

func TestRangeMap_MinOffsetAfter(t *testing.T) {
	m := &RangeMap{}
	_ = m.Add(Range{Start: 10, End: 20, Owner: 0})
	_ = m.Add(Range{Start: 30, End: 40, Owner: 1})
	_ = m.Add(Range{Start: 50, End: 60, Owner: 2})
	if got := m.MinOffsetOfOwnersAtOrAfter(1); got != 30 {
		t.Errorf("got %d, want 30", got)
	}
}

func TestRangeMap_AnyRangeAtOrAfter(t *testing.T) {
	m := &RangeMap{}
	_ = m.Add(Range{Start: 10, End: 20, Owner: 0, What: "a"})
	_ = m.Add(Range{Start: 100, End: 110, Owner: 0, What: "b"})
	r, ok := m.AnyRangeOfOwnerAtOrAfter(0, 50)
	if !ok {
		t.Fatal("expected a range")
	}
	if r.Start != 100 {
		t.Errorf("got %d, want 100", r.Start)
	}
}
