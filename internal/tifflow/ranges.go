package tifflow

import (
	"fmt"
	"sort"
)

// Range is a half-open byte range [Start, End) owned by a chain-order IFD
// index. What is a short label for debugging ("ifd", "StripOffsets",
// "strip[3]", etc.).
type Range struct {
	Start uint64
	End   uint64
	Owner int
	What  string
}

// RangeMap holds non-overlapping byte ranges, kept sorted by Start.
type RangeMap struct {
	ranges []Range
}

// Add inserts r, returning ErrOverlap if it overlaps any existing range.
func (m *RangeMap) Add(r Range) error {
	if r.End <= r.Start {
		return fmt.Errorf("invalid range [%d, %d)", r.Start, r.End)
	}
	idx := sort.Search(len(m.ranges), func(i int) bool {
		return m.ranges[i].Start >= r.Start
	})
	if idx > 0 && m.ranges[idx-1].End > r.Start {
		return fmt.Errorf("%w: new %s[%d,%d) overlaps existing %s[%d,%d)",
			ErrOverlap, r.What, r.Start, r.End,
			m.ranges[idx-1].What, m.ranges[idx-1].Start, m.ranges[idx-1].End)
	}
	if idx < len(m.ranges) && m.ranges[idx].Start < r.End {
		return fmt.Errorf("%w: new %s[%d,%d) overlaps existing %s[%d,%d)",
			ErrOverlap, r.What, r.Start, r.End,
			m.ranges[idx].What, m.ranges[idx].Start, m.ranges[idx].End)
	}
	m.ranges = append(m.ranges, Range{})
	copy(m.ranges[idx+1:], m.ranges[idx:])
	m.ranges[idx] = r
	return nil
}

// All returns all ranges in sorted order.
func (m *RangeMap) All() []Range { return m.ranges }

// MinOffsetOfOwner returns the lowest start offset among ranges owned by
// owner. Returns ^uint64(0) if no ranges for owner.
func (m *RangeMap) MinOffsetOfOwner(owner int) uint64 {
	min := ^uint64(0)
	for _, r := range m.ranges {
		if r.Owner == owner && r.Start < min {
			min = r.Start
		}
	}
	return min
}

// MinOffsetOfOwnersAtOrAfter returns the lowest start offset among ranges
// owned by any owner >= minOwner.
func (m *RangeMap) MinOffsetOfOwnersAtOrAfter(minOwner int) uint64 {
	min := ^uint64(0)
	for _, r := range m.ranges {
		if r.Owner >= minOwner && r.Start < min {
			min = r.Start
		}
	}
	return min
}

// AnyRangeOfOwnerAtOrAfter returns the first range (by start offset) owned
// by owner whose start is >= floor.
func (m *RangeMap) AnyRangeOfOwnerAtOrAfter(owner int, floor uint64) (Range, bool) {
	for _, r := range m.ranges {
		if r.Owner == owner && r.Start >= floor {
			return r, true
		}
	}
	return Range{}, false
}
