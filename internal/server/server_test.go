package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"pack-calculator/internal/store"
)

func newTestServer(t *testing.T) (*Server, *store.MemoryStore) {
	t.Helper()
	mem := store.NewMemoryStore()
	web := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html>ok</html>")},
	}
	s := New(mem, web, nil)
	return s, mem
}

func do(t *testing.T, srv *Server, method, path string, body interface{}) (*http.Response, []byte) {
	t.Helper()
	var buf io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		buf = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	resp := rec.Result()
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return resp, respBody
}

func TestGetPackSizes_Empty(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, body := do(t, srv, http.MethodGet, "/api/pack-sizes", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", resp.StatusCode, body)
	}
	var got packSizesResponse
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.PackSizes == nil {
		t.Error("pack_sizes should be [] not null")
	}
	if len(got.PackSizes) != 0 {
		t.Errorf("got %v, want []", got.PackSizes)
	}
}

func TestSetPackSizes_RoundTrip(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, body := do(t, srv, http.MethodPut, "/api/pack-sizes",
		setPackSizesRequest{PackSizes: []int{500, 250, 1000}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d; body=%s", resp.StatusCode, body)
	}
	var got packSizesResponse
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	want := []int{250, 500, 1000}
	if !equalInts(got.PackSizes, want) {
		t.Errorf("got %v, want %v", got.PackSizes, want)
	}
}

func TestSetPackSizes_RejectsInvalidSize(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, body := do(t, srv, http.MethodPut, "/api/pack-sizes",
		setPackSizesRequest{PackSizes: []int{250, 0, 500}})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400; body=%s", resp.StatusCode, body)
	}
}

func TestSetPackSizes_RejectsUnknownFields(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodPut, "/api/pack-sizes",
		bytes.NewReader([]byte(`{"pack_sizes":[1,2],"foo":"bar"}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", rec.Code)
	}
}

func TestCalculate_DefaultPackSizes(t *testing.T) {
	srv, mem := newTestServer(t)
	_ = mem.SetPackSizes(context.Background(), []int{250, 500, 1000, 2000, 5000})

	resp, body := do(t, srv, http.MethodPost, "/api/calculate", calculateRequest{Order: 12001})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d; body=%s", resp.StatusCode, body)
	}
	var got calculateResponse
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Order != 12001 {
		t.Errorf("order: got %d, want 12001", got.Order)
	}
	if got.ShippedItems != 12250 {
		t.Errorf("shipped: got %d, want 12250", got.ShippedItems)
	}
	if got.TotalPacks != 4 {
		t.Errorf("total packs: got %d, want 4", got.TotalPacks)
	}
	wantLines := []packLine{
		{Size: 5000, Quantity: 2},
		{Size: 2000, Quantity: 1},
		{Size: 250, Quantity: 1},
	}
	if len(got.Packs) != len(wantLines) {
		t.Fatalf("packs: got %v, want %v", got.Packs, wantLines)
	}
	for i, line := range got.Packs {
		if line != wantLines[i] {
			t.Errorf("pack[%d]: got %v, want %v", i, line, wantLines[i])
		}
	}
}

func TestCalculate_LargeOrderNonGreedy(t *testing.T) {
	srv, mem := newTestServer(t)
	_ = mem.SetPackSizes(context.Background(), []int{23, 31, 53})

	resp, body := do(t, srv, http.MethodPost, "/api/calculate", calculateRequest{Order: 500_000})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d; body=%s", resp.StatusCode, body)
	}
	var got calculateResponse
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.ShippedItems != 500_000 {
		t.Errorf("shipped: got %d, want 500000", got.ShippedItems)
	}
	if got.TotalPacks != 9438 {
		t.Errorf("total packs: got %d, want 9438", got.TotalPacks)
	}
	wantQty := map[int]int{23: 2, 31: 7, 53: 9429}
	for _, line := range got.Packs {
		if wantQty[line.Size] != line.Quantity {
			t.Errorf("size %d: got qty %d, want %d", line.Size, line.Quantity, wantQty[line.Size])
		}
	}
}

func TestCalculate_NoPackSizes(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, _ := do(t, srv, http.MethodPost, "/api/calculate", calculateRequest{Order: 100})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", resp.StatusCode)
	}
}

func TestCalculate_InvalidOrder(t *testing.T) {
	srv, mem := newTestServer(t)
	_ = mem.SetPackSizes(context.Background(), []int{250})
	for _, order := range []int{0, -1} {
		resp, _ := do(t, srv, http.MethodPost, "/api/calculate", calculateRequest{Order: order})
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("order %d: got %d, want 400", order, resp.StatusCode)
		}
	}
}

func TestHealth(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, body := do(t, srv, http.MethodGet, "/healthz", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, body=%s", resp.StatusCode, body)
	}
}

func TestStaticIndex(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, body := do(t, srv, http.MethodGet, "/", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d", resp.StatusCode)
	}
	if !bytes.Contains(body, []byte("<html>ok</html>")) {
		t.Errorf("expected stubbed index.html, got %q", body)
	}
}

func TestMethodRouting(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, _ := do(t, srv, http.MethodDelete, "/api/pack-sizes", nil)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("got %d, want 405", resp.StatusCode)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
