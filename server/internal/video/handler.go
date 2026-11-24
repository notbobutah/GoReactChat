package video

import (
	"encoding/json"
	"net/http"
	"slices"
	"time"
)

const downloadExpiry = 15 * time.Minute

// Register adds the portfolio routes to mux.
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/videos", s.handleList)
	mux.HandleFunc("GET /api/videos/{id}/download", s.handleDownload)
}

func (s *Service) handleList(w http.ResponseWriter, r *http.Request) {
	videos, err := s.List(r.Context())
	if err != nil {
		writeUnavailable(w)
		return
	}
	if videos == nil {
		videos = []Video{}
	}
	writeJSON(w, http.StatusOK, struct {
		Videos []Video `json:"videos"`
	}{videos})
}

// handleDownload redirects to a presigned GET. The object key always comes from
// the listing; the id in the URL is only ever compared.
func (s *Service) handleDownload(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	id := r.PathValue("id")
	if !validID(id) {
		writeError(w, http.StatusBadRequest, "invalid video id")
		return
	}
	videos, err := s.List(r.Context())
	if err != nil {
		writeUnavailable(w)
		return
	}
	i := slices.IndexFunc(videos, func(v Video) bool { return v.ID == id })
	if i < 0 {
		writeError(w, http.StatusNotFound, "video not found")
		return
	}
	location, err := s.store.presignGet(r.Context(), videos[i].key, downloadExpiry,
		`attachment; filename="expona-`+id+`.mp4"`)
	if err != nil {
		s.logger.Error("video download could not be presigned", "id", id, "error", err)
		writeUnavailable(w)
		return
	}
	http.Redirect(w, r, location, http.StatusFound)
}

// writeUnavailable is deliberately generic; the cause is in the log.
func writeUnavailable(w http.ResponseWriter) {
	writeError(w, http.StatusServiceUnavailable, "videos are temporarily unavailable")
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, struct {
		Error string `json:"error"`
	}{message})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
