package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

func writeTxErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, errNotAnchored):
		writeError(w, http.StatusUnprocessableEntity, "daemon is not anchored")
	default:
		log.Printf("api: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}
