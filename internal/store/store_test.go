package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

// runStoreSuite is the shared contract test — both implementations must satisfy
// it identically so they can be swapped without behaviour changes.
func runStoreSuite(t *testing.T, factory func(t *testing.T) Store) {
	t.Helper()
	ctx := context.Background()

	t.Run("starts empty", func(t *testing.T) {
		s := factory(t)
		got, err := s.GetPackSizes(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Errorf("want empty, got %v", got)
		}
	})

	t.Run("set then get", func(t *testing.T) {
		s := factory(t)
		if err := s.SetPackSizes(ctx, []int{500, 250, 1000}); err != nil {
			t.Fatal(err)
		}
		got, err := s.GetPackSizes(ctx)
		if err != nil {
			t.Fatal(err)
		}
		want := []int{250, 500, 1000}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("set replaces previous", func(t *testing.T) {
		s := factory(t)
		_ = s.SetPackSizes(ctx, []int{1, 2, 3})
		if err := s.SetPackSizes(ctx, []int{42}); err != nil {
			t.Fatal(err)
		}
		got, _ := s.GetPackSizes(ctx)
		if !reflect.DeepEqual(got, []int{42}) {
			t.Errorf("got %v, want [42]", got)
		}
	})

	t.Run("dedupes input", func(t *testing.T) {
		s := factory(t)
		_ = s.SetPackSizes(ctx, []int{500, 500, 250, 250, 500})
		got, _ := s.GetPackSizes(ctx)
		if !reflect.DeepEqual(got, []int{250, 500}) {
			t.Errorf("got %v, want [250 500]", got)
		}
	})

	t.Run("rejects non-positive sizes", func(t *testing.T) {
		s := factory(t)
		err := s.SetPackSizes(ctx, []int{250, 0, 500})
		if !errors.Is(err, ErrInvalidPackSize) {
			t.Errorf("want ErrInvalidPackSize, got %v", err)
		}
		got, _ := s.GetPackSizes(ctx)
		if len(got) != 0 {
			t.Errorf("state mutated despite error: %v", got)
		}
	})

	t.Run("empty set is valid", func(t *testing.T) {
		s := factory(t)
		_ = s.SetPackSizes(ctx, []int{250, 500})
		if err := s.SetPackSizes(ctx, []int{}); err != nil {
			t.Errorf("empty set should be valid: %v", err)
		}
		got, _ := s.GetPackSizes(ctx)
		if len(got) != 0 {
			t.Errorf("got %v, want empty", got)
		}
	})
}

func TestMemoryStore(t *testing.T) {
	runStoreSuite(t, func(t *testing.T) Store {
		return NewMemoryStore()
	})
}

func TestSQLiteStore(t *testing.T) {
	runStoreSuite(t, func(t *testing.T) Store {
		dbPath := filepath.Join(t.TempDir(), "test.db")
		s, err := NewSQLiteStore(dbPath)
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		t.Cleanup(func() { _ = s.Close() })
		return s
	})
}

func TestSQLiteStore_Persistence(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "persist.db")

	s1, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := s1.SetPackSizes(context.Background(), []int{23, 31, 53}); err != nil {
		t.Fatal(err)
	}
	if err := s1.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	got, err := s2.GetPackSizes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []int{23, 31, 53}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// Defaults must populate only an empty DB — otherwise an operator's runtime
// change would silently revert on every redeploy.
func TestSQLiteStore_SeedIfEmpty(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "seed.db")
	s, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.SeedIfEmpty(ctx, []int{250, 500, 1000, 2000, 5000}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetPackSizes(ctx)
	if !reflect.DeepEqual(got, []int{250, 500, 1000, 2000, 5000}) {
		t.Errorf("first seed: got %v", got)
	}

	_ = s.SetPackSizes(ctx, []int{42})

	if err := s.SeedIfEmpty(ctx, []int{250, 500, 1000, 2000, 5000}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetPackSizes(ctx)
	if !reflect.DeepEqual(got, []int{42}) {
		t.Errorf("second seed clobbered operator config: got %v", got)
	}
}

func TestSQLiteStore_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "concurrent.db")
	s, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_ = s.SetPackSizes(ctx, []int{250, 500})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = s.GetPackSizes(ctx)
		}()
		go func(i int) {
			defer wg.Done()
			_ = s.SetPackSizes(ctx, []int{100 + i})
		}(i)
	}
	wg.Wait()
}
