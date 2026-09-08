package api

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/linell/aether/internal/policy"
	"github.com/linell/aether/internal/store"
)

type server struct {
	store    *store.Store
	rules    []policy.Rule
	telegram Telegram
}

type Options struct {
	Token    string
	Rules    []policy.Rule
	Telegram Telegram
}

const telegramWebhookPath = "/v1/channels/telegram/webhook"

func New(st *store.Store, o Options) (http.Handler, error) {
	if o.Token == "" {
		return nil, errors.New("api: token must not be empty")
	}
	s := &server{store: st, rules: o.Rules, telegram: o.Telegram}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.Handle("/v1/", http.StripPrefix("/v1", s.v1()))
	return withAuth(mux, o.Token), nil
}

func (s *server) v1() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /channels/telegram/webhook", s.handleTelegramWebhook)
	mux.HandleFunc("GET /threads/{id}", s.handleGetThread)
	mux.HandleFunc("GET /threads/{id}/messages", s.handleListMessages)
	mux.HandleFunc("POST /threads/{id}/reply", s.handleThreadReply)
	mux.HandleFunc("POST /threads/{id}/approvals", s.handleThreadApprovals)
	mux.HandleFunc("GET /approvals/{id}", s.handleGetApproval)
	mux.HandleFunc("GET /approvals/groups/{id}", s.handleGetApprovalGroup)
	mux.HandleFunc("POST /approvals/{id}/answer", s.handleAnswerApproval)
	mux.HandleFunc("POST /daemons", s.handleCreateDaemon)
	mux.HandleFunc("GET /daemons/{name}", s.handleGetDaemon)
	mux.HandleFunc("PATCH /daemons/{name}", s.handlePatchDaemon)
	mux.HandleFunc("POST /daemons/{name}/messages", s.handleCreateMessage)
	mux.HandleFunc("POST /daemons/{name}/markers", s.handleCreateMarker)
	mux.HandleFunc("POST /daemons/{name}/allowlist/match", s.handleMatchCall)
	mux.HandleFunc("GET /daemons/{name}/soul", s.handleGetSoul)
	mux.HandleFunc("GET /daemons/{name}/memory", s.handleGetMemory)
	mux.HandleFunc("PUT /daemons/{name}/memory", s.handlePutMemory)
	mux.HandleFunc("GET /daemons/{name}/schedules", s.handleListSchedules)
	mux.HandleFunc("PUT /daemons/{name}/schedules/{id}", s.handlePutSchedule)
	mux.HandleFunc("DELETE /daemons/{name}/schedules/{id}", s.handleDeleteSchedule)
	mux.HandleFunc("POST /operations", s.handleClaimOperation)
	mux.HandleFunc("POST /hosts", s.handleCreateHost)
	mux.HandleFunc("GET /hosts/{name}/daemons", s.handleHostDaemons)
	return mux
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
	healthz := r.Method == http.MethodGet && r.URL.Path == "/healthz"
	webhook := r.Method == http.MethodPost && r.URL.Path == telegramWebhookPath
	return healthz || webhook
}

func bearerMatches(r *http.Request, token string) bool {
	supplied, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && subtle.ConstantTimeCompare([]byte(supplied), []byte(token)) == 1
}
