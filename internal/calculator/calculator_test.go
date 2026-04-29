package calculator

import (
	"errors"
	"testing"
)

func TestCalculate_BriefExamples(t *testing.T) {
	defaultSizes := []int{250, 500, 1000, 2000, 5000}

	cases := []struct {
		name          string
		order         int
		wantItems     int
		wantPacks     int
		wantBreakdown Result
	}{
		{"order=1 -> 1x250", 1, 250, 1, Result{250: 1}},
		{"order=250 -> 1x250", 250, 250, 1, Result{250: 1}},
		{"order=251 -> 1x500 (not 2x250)", 251, 500, 1, Result{500: 1}},
		{"order=501 -> 1x500+1x250 (not 1x1000, not 3x250)", 501, 750, 2, Result{500: 1, 250: 1}},
		{"order=12001 -> 2x5000+1x2000+1x250", 12001, 12250, 4, Result{5000: 2, 2000: 1, 250: 1}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Calculate(defaultSizes, tc.order)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.TotalItems() != tc.wantItems {
				t.Errorf("items: got %d, want %d (%v)", got.TotalItems(), tc.wantItems, got)
			}
			if got.TotalPacks() != tc.wantPacks {
				t.Errorf("packs: got %d, want %d (%v)", got.TotalPacks(), tc.wantPacks, got)
			}
			if !equalResult(got, tc.wantBreakdown) {
				t.Errorf("breakdown: got %v, want %v", got, tc.wantBreakdown)
			}
		})
	}
}

// Large-order edge case where a naive greedy fails: filling with the
// largest pack (53) leaves a remainder of 51, which cannot be made from
// {23, 31}, so greedy either stalls or overshoots the optimum.
func TestCalculate_LargeOrderNonGreedy(t *testing.T) {
	got, err := Calculate([]int{23, 31, 53}, 500_000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := Result{23: 2, 31: 7, 53: 9429}
	if !equalResult(got, want) {
		t.Fatalf("breakdown: got %v, want %v", got, want)
	}
	if got.TotalItems() != 500_000 {
		t.Errorf("items: got %d, want 500000", got.TotalItems())
	}
	if got.TotalPacks() != 9438 {
		t.Errorf("packs: got %d, want 9438", got.TotalPacks())
	}
}

// gcd(6,10)=2 forces an even total; 14 is even but unreachable from {6,10},
// so the smallest valid total is 16.
func TestCalculate_NotExactlyReachable(t *testing.T) {
	got, err := Calculate([]int{6, 10}, 13)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TotalItems() != 16 {
		t.Errorf("items: got %d, want 16 (%v)", got.TotalItems(), got)
	}
	if got.TotalPacks() != 2 {
		t.Errorf("packs: got %d, want 2", got.TotalPacks())
	}
}

func TestCalculate_PrefersFewerPacksWithEqualItems(t *testing.T) {
	got, err := Calculate([]int{250, 500}, 500)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := Result{500: 1}
	if !equalResult(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCalculate_SinglePackSize(t *testing.T) {
	got, err := Calculate([]int{7}, 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := Result{7: 3}
	if !equalResult(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCalculate_DuplicatesAndUnsorted(t *testing.T) {
	got, err := Calculate([]int{500, 250, 500, 1000, 250}, 251)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := Result{500: 1}
	if !equalResult(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCalculate_ValidationErrors(t *testing.T) {
	cases := []struct {
		name    string
		sizes   []int
		order   int
		wantErr error
	}{
		{"empty pack sizes", []int{}, 100, ErrNoPackSizes},
		{"zero order", []int{250}, 0, ErrInvalidOrder},
		{"negative order", []int{250}, -5, ErrInvalidOrder},
		{"zero pack size", []int{0, 250}, 100, ErrInvalidPackSize},
		{"negative pack size", []int{-1, 250}, 100, ErrInvalidPackSize},
		{"order over cap", []int{250}, maxOrder + 1, ErrOrderOutOfBounds},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Calculate(tc.sizes, tc.order)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestCalculate_OutputIsConsistent(t *testing.T) {
	cases := []struct {
		sizes []int
		order int
	}{
		{[]int{250, 500, 1000, 2000, 5000}, 1},
		{[]int{250, 500, 1000, 2000, 5000}, 12001},
		{[]int{23, 31, 53}, 500_000},
		{[]int{6, 10}, 13},
	}
	for _, tc := range cases {
		got, err := Calculate(tc.sizes, tc.order)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		sum := 0
		count := 0
		for size, qty := range got {
			sum += size * qty
			count += qty
		}
		if sum != got.TotalItems() {
			t.Errorf("items mismatch: breakdown sums to %d, TotalItems=%d", sum, got.TotalItems())
		}
		if count != got.TotalPacks() {
			t.Errorf("packs mismatch: breakdown counts to %d, TotalPacks=%d", count, got.TotalPacks())
		}
		if sum < tc.order {
			t.Errorf("under-shipped: sum=%d < order=%d", sum, tc.order)
		}
	}
}

func equalResult(a, b Result) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func BenchmarkCalculate_LargeOrderNonGreedy(b *testing.B) {
	sizes := []int{23, 31, 53}
	for i := 0; i < b.N; i++ {
		_, _ = Calculate(sizes, 500_000)
	}
}
