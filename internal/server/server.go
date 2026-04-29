// Package server wires the HTTP layer: JSON API + static UI.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"time"

	"pack-calculator/internal/calculator"
	"pack-calculator/internal/store"
)

type Server struct {
	store  store.Store
	web    fs.FS
	logger *log.Logger
}

func New(s store.Store, web fs.FS, logger *log.Logger) *Server {
	if logger == nil {
		logger = log.Default()
	}
	return &Server{store: s, web: web, logger: logger}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/pack-sizes", s.handleGetPackSizes)
	mux.HandleFunc("PUT /api/pack-sizes", s.handleSetPackSizes)
	mux.HandleFunc("POST /api/calculate", s.handleCalculate)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.Handle("GET /", http.FileServerFS(s.web))
	return s.recoverPanics(s.logRequests(mux))
}

type packSizesResponse struct {
	PackSizes []int `json:"pack_sizes"`
}

type setPackSizesRequest struct {
	PackSizes []int `json:"pack_sizes"`
}

type calculateRequest struct {
	Order int `json:"order"`
}

type calculateResponse struct {
	Order        int        `json:"order"`
	ShippedItems int        `json:"shipped_items"`
	TotalPacks   int        `json:"total_packs"`
	Packs        []packLine `json:"packs"`
	UsedSizes    []int      `json:"used_pack_sizes"`
}

type packLine struct {
	Size     int `json:"size"`
	Quantity int `json:"quantity"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func (s *Server) handleGetPackSizes(w http.ResponseWriter, r *http.Request) {
	sizes, err := s.store.GetPackSizes(r.Context())
	if err != nil {
		s.logger.Printf("get pack sizes: %v", err)
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "failed to read pack sizes"})
		return
	}
	if sizes == nil {
		sizes = []int{}
	}
	writeJSON(w, http.StatusOK, packSizesResponse{PackSizes: sizes})
}

func (s *Server) handleSetPackSizes(w http.ResponseWriter, r *http.Request) {
	var req setPackSizesRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	if err := s.store.SetPackSizes(r.Context(), req.PackSizes); err != nil {
		if errors.Is(err, store.ErrInvalidPackSize) {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
			return
		}
		s.logger.Printf("set pack sizes: %v", err)
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "failed to save pack sizes"})
		return
	}
	saved, _ := s.store.GetPackSizes(r.Context())
	if saved == nil {
		saved = []int{}
	}
	writeJSON(w, http.StatusOK, packSizesResponse{PackSizes: saved})
}

func (s *Server) handleCalculate(w http.ResponseWriter, r *http.Request) {
	var req calculateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}

	sizes, err := s.store.GetPackSizes(r.Context())
	if err != nil {
		s.logger.Printf("calc: load pack sizes: %v", err)
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "failed to read pack sizes"})
		return
	}

	result, err := calculator.Calculate(sizes, req.Order)
	if err != nil {
		switch {
		case errors.Is(err, calculator.ErrNoPackSizes),
			errors.Is(err, calculator.ErrInvalidOrder),
			errors.Is(err, calculator.ErrInvalidPackSize),
			errors.Is(err, calculator.ErrOrderOutOfBounds):
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		default:
			s.logger.Printf("calc: %v", err)
			writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "calculation failed"})
		}
		return
	}

	writeJSON(w, http.StatusOK, buildCalculateResponse(req.Order, result))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if _, err := s.store.GetPackSizes(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "down"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func buildCalculateResponse(order int, r calculator.Result) calculateResponse {
	usedSizes := make([]int, 0, len(r))
	for size := range r {
		usedSizes = append(usedSizes, size)
	}
	for i := 1; i < len(usedSizes); i++ {
		for j := i; j > 0 && usedSizes[j] > usedSizes[j-1]; j-- {
			usedSizes[j], usedSizes[j-1] = usedSizes[j-1], usedSizes[j]
		}
	}
	lines := make([]packLine, 0, len(usedSizes))
	for _, size := range usedSizes {
		lines = append(lines, packLine{Size: size, Quantity: r[size]})
	}
	return calculateResponse{
		Order:        order,
		ShippedItems: r.TotalItems(),
		TotalPacks:   r.TotalPacks(),
		Packs:        lines,
		UsedSizes:    usedSizes,
	}
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		s.logger.Printf("%s %s (%s)", r.Method, r.URL.Path, time.Since(start))
	})
}

func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.logger.Printf("panic on %s %s: %v", r.Method, r.URL.Path, rec)
				writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// decodeJSON rejects unknown fields so JSON-key typos surface as 400s rather
// than being silently dropped.
func decodeJSON(r *http.Request, v interface{}) error {
	if r.Body == nil {
		return errors.New("missing request body")
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
