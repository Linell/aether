package api

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/linell/aether/internal/store"
)

type server struct {
	store *store.Store
}

func New(st *store.Store, token string) (http.Handler, error) {
	if token == "" {
		return nil, errors.New("api: token must not be empty")
	}
	s := &server{store: st}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.Handle("/v1/", http.StripPrefix("/v1", s.v1()))
	return withAuth(mux, token), nil
}

func (s *server) v1() http.Handler {
	return http.NewServeMux()
}

func (s *server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte("ok"))
}

func withAuth(next http.Handler, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublic(r) || bearerMatches(r, token) {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

func isPublic(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/healthz"
}

func bearerMatches(r *http.Request, token string) bool {
	supplied, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && subtle.ConstantTimeCompare([]byte(supplied), []byte(token)) == 1
}
