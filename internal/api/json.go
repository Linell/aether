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

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return false
	}
	return true
}

func decodeRaw(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return errors.New("empty")
	}
	return json.Unmarshal(raw, v)
}

func requireFields(w http.ResponseWriter, fields ...string) bool {
	for _, f := range fields {
		if f == "" {
			writeError(w, http.StatusBadRequest, "missing required field")
			return false
		}
	}
	return true
}

func respond(w http.ResponseWriter, status int, v any, err error) {
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, status, v)
}

func writeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, errNotAnchored):
		writeError(w, http.StatusUnprocessableEntity, "daemon is not anchored")
	case errors.Is(err, errForeignThread):
		writeError(w, http.StatusBadRequest, errForeignThread.Error())
	case errors.Is(err, errConflict):
		writeError(w, http.StatusConflict, "conflict")
	default:
		log.Printf("api: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}
