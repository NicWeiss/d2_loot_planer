// Package httpapi is the HTTP interface: the JSON API under /api and the static front end at /.
// Handlers are thin: parse the request, call a use case or a query, write its result.
package httpapi

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"loot-planner/internal/application"
	"loot-planner/internal/domain/stash"
)

const (
	sessionCookie = "lp_session"
	maxBody       = 8 << 20 // a stash export is about 1 MB
)

type Server struct {
	Market       *application.MarketService
	Stash        *application.StashService
	Queries      *application.Queries
	Access       *application.Access
	Frontend     fs.FS
	CookieSecure bool
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/meta", s.meta)
	mux.HandleFunc("GET /api/market/status", s.marketStatus)
	mux.HandleFunc("POST /api/market/refresh", s.marketRefresh)
	mux.HandleFunc("GET /api/realms/{realm}/targets", s.realmQuery(func(r *http.Request, realm string) (any, error) { return s.Queries.Targets(realm) }))
	mux.HandleFunc("GET /api/realms/{realm}/target", s.realmQuery(func(r *http.Request, realm string) (any, error) {
		return s.Queries.Target(realm, r.URL.Query().Get("code"), s.session(r))
	}))
	mux.HandleFunc("GET /api/realms/{realm}/offers", s.realmQuery(func(r *http.Request, realm string) (any, error) {
		return s.Queries.Offers(realm, r.URL.Query().Get("code"))
	}))
	mux.HandleFunc("POST /api/realms/{realm}/route", s.route)
	mux.HandleFunc("POST /api/realms/{realm}/strategy", s.strategy)
	mux.HandleFunc("GET /api/realms/{realm}/rates", s.realmQuery(func(r *http.Request, realm string) (any, error) { return s.Queries.Rates(realm) }))
	mux.HandleFunc("GET /api/realms/{realm}/map", s.realmQuery(func(r *http.Request, realm string) (any, error) { return s.Queries.Map(realm) }))
	mux.HandleFunc("GET /api/realms/{realm}/deals", s.realmQuery(func(r *http.Request, realm string) (any, error) { return s.Queries.Deals(realm) }))
	mux.HandleFunc("GET /api/realms/{realm}/stash", s.realmQuery(func(r *http.Request, realm string) (any, error) {
		return s.Queries.Stash(realm, s.session(r))
	}))
	mux.HandleFunc("POST /api/stash/login", s.stashLogin)
	mux.HandleFunc("POST /api/stash/refresh", s.stashRefresh)
	mux.HandleFunc("POST /api/stash/upload", s.stashUpload)
	mux.HandleFunc("DELETE /api/stash", s.stashReset)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "нет такого метода")
	})
	mux.Handle("/", http.FileServerFS(s.Frontend))
	return guard(mux)
}

// guard: JSON bodies force a CORS preflight that this API never grants, so other sites cannot post here;
// responses are not cached so a new build or snapshot shows on the next load; request lines are logged, bodies never.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Method != http.MethodGet && r.Method != http.MethodHead {
			if ct, _, _ := strings.Cut(r.Header.Get("Content-Type"), ";"); strings.TrimSpace(ct) != "application/json" {
				writeError(w, http.StatusUnsupportedMediaType, "нужен JSON")
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		}
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		if r.URL.Path != "/api/health" {
			slog.Info("http", "method", r.Method, "path", r.URL.Path, "status", rec.status, "ms", time.Since(start).Milliseconds())
		}
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) { r.status = code; r.ResponseWriter.WriteHeader(code) }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("encode response", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// fail maps use-case errors to HTTP statuses.
func fail(w http.ResponseWriter, err error) {
	var (
		unknown *application.UnknownRealmError
		refused *application.RefreshRefusedError
		expired *application.LoginExpiredError
		site    *application.SiteError
		tooBig  *http.MaxBytesError
	)
	switch {
	case errors.Is(err, application.ErrMarketNotReady):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	case errors.As(err, &unknown):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, application.ErrForbidden):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.As(err, &refused):
		writeError(w, http.StatusConflict, err.Error())
	case errors.As(err, &expired):
		writeError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, application.ErrNoStash):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, stash.ErrNotAStashExport):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.As(err, &site):
		status := http.StatusBadGateway
		if site.Unauthorized() {
			status = http.StatusUnauthorized
		}
		writeError(w, status, "сайт: "+site.Msg)
	case errors.As(err, &tooBig):
		writeError(w, http.StatusRequestEntityTooLarge, "слишком большой запрос")
	default:
		slog.Error("request failed", "err", err)
		writeError(w, http.StatusInternalServerError, "внутренняя ошибка")
	}
}

func (s *Server) session(r *http.Request) *application.Session {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil
	}
	return s.Stash.Get(c.Value)
}

func sessionID(r *http.Request) string {
	if c, err := r.Cookie(sessionCookie); err == nil {
		return c.Value
	}
	return ""
}

func (s *Server) setSession(w http.ResponseWriter, id string) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: id, Path: "/", MaxAge: int(s.Stash.TTL.Seconds()),
		HttpOnly: true, Secure: s.CookieSecure, SameSite: http.SameSiteStrictMode})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			fail(w, err)
		} else {
			writeError(w, http.StatusUnprocessableEntity, "битый JSON")
		}
		return false
	}
	return true
}

// ---- handlers

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]bool{"ok": true, "ready": s.Market.State() != nil})
}

func (s *Server) meta(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.Queries.Meta(s.session(r)))
}

func (s *Server) marketStatus(w http.ResponseWriter, r *http.Request) {
	var fetched any
	if st := s.Market.State(); st != nil && st.FetchedAt != "" {
		fetched = st.FetchedAt
	}
	writeJSON(w, 200, map[string]any{"job": s.Market.Job(), "fetchedAt": fetched})
}

func (s *Server) marketRefresh(w http.ResponseWriter, r *http.Request) {
	if !s.Access.IsAdmin(s.session(r)) {
		fail(w, application.ErrForbidden)
		return
	}
	job, err := s.Market.Refresh(true)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job": job})
}

func (s *Server) realmQuery(q func(r *http.Request, realm string) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v, err := q(r, r.PathValue("realm"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, v)
	}
}

func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	if p, ok := routeParams(w, r); ok {
		v, err := s.Queries.Route(r.PathValue("realm"), p, s.session(r))
		reply(w, v, err)
	}
}

func (s *Server) strategy(w http.ResponseWriter, r *http.Request) {
	if p, ok := routeParams(w, r); ok {
		v, err := s.Queries.Strategy(r.PathValue("realm"), p, s.session(r))
		reply(w, v, err)
	}
}

func reply(w http.ResponseWriter, v any, err error) {
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, v)
}

// routeParams reads and checks a route or strategy request; on false the error is already written.
func routeParams(w http.ResponseWriter, r *http.Request) (application.RouteParams, bool) {
	body := struct {
		Target    string   `json:"target"`
		Qty       int      `json:"qty"`
		StashMode string   `json:"stashMode"`
		Keys      []string `json:"keys"`
		UseCube   *bool    `json:"useCube"`
		Effort    *float64 `json:"effort"`
	}{Qty: 1, StashMode: "all"}
	if !decode(w, r, &body) {
		return application.RouteParams{}, false
	}
	modes := map[string]bool{"all": true, "currency": true, "gems": true, "none": true}
	switch {
	case body.Target == "" || len(body.Target) > 300:
		writeError(w, 422, "target: от 1 до 300 символов")
		return application.RouteParams{}, false
	case body.Qty < 1 || body.Qty > 20:
		writeError(w, 422, "qty: от 1 до 20")
		return application.RouteParams{}, false
	case !modes[body.StashMode]:
		writeError(w, 422, "stashMode: all, currency, gems или none")
		return application.RouteParams{}, false
	case body.Effort != nil && (*body.Effort < 0 || *body.Effort > 1):
		writeError(w, 422, "effort: от 0 до 1")
		return application.RouteParams{}, false
	}
	for _, k := range body.Keys {
		if k != "pk1" && k != "pk2" && k != "pk3" {
			writeError(w, 422, "keys: только pk1, pk2, pk3")
			return application.RouteParams{}, false
		}
	}
	p := application.RouteParams{Target: body.Target, Qty: body.Qty, StashMode: body.StashMode, Keys: body.Keys,
		UseCube: body.UseCube == nil || *body.UseCube, Effort: 0.03}
	if body.Effort != nil {
		p.Effort = *body.Effort
	}
	return p, true
}

func (s *Server) stashReply(w http.ResponseWriter, sess *application.Session, warning string) {
	s.setSession(w, sess.ID)
	var warn any
	if warning != "" {
		warn = warning
	}
	st := sess.Stash
	writeJSON(w, 200, map[string]any{"owner": st.Owner, "source": st.Source, "fetchedAt": st.FetchedAt,
		"remembered": sess.Remembered(), "realms": st.RealmKeys(), "warning": warn})
}

func (s *Server) stashLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Login    string `json:"login"`
		Password string `json:"password"`
		Remember bool   `json:"remember"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Login == "" || len(body.Login) > 100 || body.Password == "" || len(body.Password) > 200 {
		writeError(w, 422, "нужны логин и пароль")
		return
	}
	sess, err := s.Stash.Login(sessionID(r), body.Login, body.Password, body.Remember)
	if err != nil {
		fail(w, err)
		return
	}
	s.stashReply(w, sess, "")
}

func (s *Server) stashRefresh(w http.ResponseWriter, r *http.Request) {
	sess, warning, err := s.Stash.Refresh(sessionID(r))
	if err != nil {
		fail(w, err)
		return
	}
	s.stashReply(w, sess, warning)
}

func (s *Server) stashUpload(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string          `json:"name"`
		Data json.RawMessage `json:"data"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Name == "" {
		body.Name = "файл"
	}
	if len(body.Name) > 100 || len(body.Data) == 0 {
		writeError(w, 422, "нужны name и data")
		return
	}
	sess, err := s.Stash.Upload(sessionID(r), body.Data, body.Name)
	if err != nil {
		fail(w, err)
		return
	}
	s.stashReply(w, sess, "")
}

func (s *Server) stashReset(w http.ResponseWriter, r *http.Request) {
	s.Stash.Reset(sessionID(r))
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, 200, map[string]bool{"ok": true})
}
