// Package calculator computes the optimal pack breakdown for an order under
// the brief's three rules: whole packs only, fewest items, then fewest packs.
//
// The implementation is a bounded unbounded-knapsack DP rather than a greedy.
// Greedy fails on inputs like {23,31,53}/500000: filling with 53s first leaves
// a remainder of 51, which cannot be made from {23,31} — so greedy either
// stalls or overshoots the optimal item count.
package calculator

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

type Result map[int]int

var (
	ErrNoPackSizes      = errors.New("no pack sizes configured")
	ErrInvalidOrder     = errors.New("order must be a positive integer")
	ErrInvalidPackSize  = errors.New("pack sizes must be positive integers")
	ErrUnreachable      = errors.New("no combination of packs can fulfil the order")
	ErrOrderOutOfBounds = errors.New("order is too large for in-memory DP")
)

// maxOrder caps DP allocation at ~400MB worst case. Well above any realistic
// order; without it an unbounded request could OOM the process.
const maxOrder = 50_000_000

func Calculate(packSizes []int, order int) (Result, error) {
	if order <= 0 {
		return nil, ErrInvalidOrder
	}
	if order > maxOrder {
		return nil, fmt.Errorf("%w: max supported order is %d", ErrOrderOutOfBounds, maxOrder)
	}

	sizes, err := normalisePackSizes(packSizes)
	if err != nil {
		return nil, err
	}
	if len(sizes) == 0 {
		return nil, ErrNoPackSizes
	}

	maxSize := sizes[0]
	upperBound := order + maxSize

	const inf = math.MaxInt32
	minPacks := make([]int, upperBound+1)
	lastPack := make([]int, upperBound+1)
	for i := range minPacks {
		minPacks[i] = inf
	}
	minPacks[0] = 0

	// Sizes are descending so on ties (multiple sizes give the same minPacks
	// value), the larger pack wins — yields fewer, bigger packs in the
	// reconstructed breakdown, matching the brief's worked examples.
	for t := 1; t <= upperBound; t++ {
		for _, s := range sizes {
			prev := t - s
			if prev < 0 || minPacks[prev] == inf {
				continue
			}
			if minPacks[prev]+1 < minPacks[t] {
				minPacks[t] = minPacks[prev] + 1
				lastPack[t] = s
			}
		}
	}

	target := -1
	for t := order; t <= upperBound; t++ {
		if minPacks[t] != inf {
			target = t
			break
		}
	}
	if target == -1 {
		return nil, ErrUnreachable
	}

	result := Result{}
	for t := target; t > 0; {
		s := lastPack[t]
		result[s]++
		t -= s
	}
	return result, nil
}

func normalisePackSizes(in []int) ([]int, error) {
	seen := make(map[int]struct{}, len(in))
	out := make([]int, 0, len(in))
	for _, s := range in {
		if s <= 0 {
			return nil, ErrInvalidPackSize
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(out)))
	return out, nil
}

func (r Result) TotalItems() int {
	total := 0
	for size, qty := range r {
		total += size * qty
	}
	return total
}

func (r Result) TotalPacks() int {
	total := 0
	for _, qty := range r {
		total += qty
	}
	return total
}
