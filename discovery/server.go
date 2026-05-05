package discovery

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/hibiken/asynq"
)

type Server struct {
	disc   *Discovery
	client *asynq.Client
}

func NewServer(disc *Discovery, client *asynq.Client) *Server {
	return &Server{disc: disc, client: client}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == "POST" && r.URL.Path == "/v1/search":
		s.handleSearch(w, r)
	case r.Method == "POST" && r.URL.Path == "/v1/register":
		s.handleRegister(w, r)
	case r.Method == "GET" && r.URL.Path == "/health":
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	var req SearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "bad request", 400)
		return
	}
	results, err := s.disc.Search(r.Context(), req)
	if err != nil {
		writeError(w, err.Error(), 500)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"results": results})
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "bad request", 400)
		return
	}
	if req.ID == "" {
		writeError(w, "id is required", 400)
		return
	}
	if req.Version == "" {
		req.Version = "latest"
	}

	if err := s.disc.RegisterSkill(r.Context(), SkillSummary{
		ID:      req.ID,
		Version: req.Version,
	}); err != nil {
		writeError(w, fmt.Sprintf("register: %s", err), 500)
		return
	}

	task, err := NewRegisterSkillTask(req.ID, req.Version)
	if err != nil {
		writeError(w, "internal error", 500)
		return
	}
	if _, err := s.client.Enqueue(task); err != nil {
		writeError(w, fmt.Sprintf("queue: %s", err), 500)
		return
	}

	json.NewEncoder(w).Encode(map[string]any{"id": req.ID, "status": "pending"})
}

func writeError(w http.ResponseWriter, msg string, code int) {
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
