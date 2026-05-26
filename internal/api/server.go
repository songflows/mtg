package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/9seconds/mtg/v2/internal/config"
	"github.com/9seconds/mtg/v2/internal/users"
	"github.com/yl2chen/cidranger"
)

const (
	defaultListen              = "127.0.0.1:9091"
	defaultBodyLimit     int64 = 65536
	apiPrefix                  = "/v1"
)

// Server is a telemt-compatible control-plane HTTP API.
type Server struct {
	httpServer *http.Server
	store      *users.Store
	conf       *config.Config
	linkBase   users.LinkSettings
	whitelist  cidranger.Ranger
	authHeader string
	readOnly   bool
	bodyLimit  int64
}

// NewServer builds an API server. store may be nil when users file is not configured.
func NewServer(conf *config.Config, store *users.Store) (*Server, error) {
	if !conf.API.Enabled.Get(false) {
		return nil, nil
	}

	listen := conf.API.Listen.Get(defaultListen)
	ranger, err := makeWhitelist(apiWhitelist(conf))
	if err != nil {
		return nil, err
	}

	linkBase := users.LinkSettings{}
	if store != nil {
		linkBase = store.LinkSettings()
	}

	linkBase = users.LinkSettingsFromIPs(
		linkBase,
		conf.PublicIPv4.Get(nil),
		conf.PublicIPv6.Get(nil),
		conf.BindTo.Port,
	)

	s := &Server{
		store:      store,
		conf:       conf,
		linkBase:   linkBase,
		whitelist:  ranger,
		authHeader: conf.API.AuthHeader,
		readOnly:   conf.API.ReadOnly.Get(false),
		bodyLimit:  int64(conf.API.RequestBodyLimitBytes.Get(uint(defaultBodyLimit))),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.serve)

	s.httpServer = &http.Server{
		Addr:              listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	return s, nil
}

// Handler returns the HTTP handler (for tests and custom servers).
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.serve)
}

// ListenAndServe starts the API listener.
func (s *Server) ListenAndServe() error {
	if s == nil {
		return nil
	}

	return s.httpServer.ListenAndServe() //nolint: wrapcheck
}

// Shutdown stops the API server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}

	return s.httpServer.Shutdown(ctx) //nolint: wrapcheck
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	if !s.allowIP(clientIP(r)) {
		writeError(w, http.StatusForbidden, "forbidden", "source IP is not allowed", "")

		return
	}

	if s.authHeader != "" && r.Header.Get("Authorization") != s.authHeader {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid Authorization header", "")

		return
	}

	path := r.URL.Path
	if path != apiPrefix && !strings.HasPrefix(path, apiPrefix+"/") {
		writeError(w, http.StatusNotFound, "not_found", "unknown route", "")

		return
	}

	switch {
	case path == apiPrefix+"/health" && r.Method == http.MethodGet:
		s.handleHealth(w, r)
	case path == apiPrefix+"/users" && r.Method == http.MethodPost:
		s.handleCreateUser(w, r)
	case strings.HasPrefix(path, apiPrefix+"/users/") && r.Method == http.MethodGet:
		s.handleGetUser(w, r, strings.TrimPrefix(path, apiPrefix+"/users/"))
	case strings.HasPrefix(path, apiPrefix+"/users/") && r.Method == http.MethodDelete:
		s.handleDeleteUser(w, r, strings.TrimPrefix(path, apiPrefix+"/users/"))
	default:
		writeError(w, http.StatusNotFound, "not_found", "unknown route", revisionOrEmpty(s))
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeOK(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"read_only": s.readOnly,
	}, revisionOrEmpty(s))
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "api_disabled", "users store is not configured", "")

		return
	}

	if s.readOnly {
		writeError(w, http.StatusForbidden, "read_only", "API is in read-only mode", s.store.Revision())

		return
	}

	body, err := readBody(r, s.bodyLimit)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), s.store.Revision())

		return
	}

	req := users.CreateUserRequest{}

	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON", s.store.Revision())

		return
	}

	user, err := s.store.Create(req, r.Header.Get("If-Match"))
	switch {
	case errors.Is(err, users.ErrUserExists):
		writeError(w, http.StatusConflict, "user_exists", err.Error(), s.store.Revision())
	case err != nil:
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), s.store.Revision())
	default:
		info := users.ToUserInfo(user, s.linkBase, time.Now())
		writeOK(w, http.StatusCreated, users.CreateUserResponse{
			User:   info,
			Secret: user.Secret.String(),
		}, s.store.Revision())
	}
}

func (s *Server) handleGetUser(w http.ResponseWriter, r *http.Request, username string) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "api_disabled", "users store is not configured", "")

		return
	}

	if username == "" || strings.Contains(username, "/") {
		writeError(w, http.StatusNotFound, "not_found", "unknown user", s.store.Revision())

		return
	}

	user, err := s.store.Get(username)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "unknown user", s.store.Revision())

		return
	}

	writeOK(w, http.StatusOK, users.ToUserInfo(user, s.linkBase, time.Now()), s.store.Revision())
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request, username string) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "api_disabled", "users store is not configured", "")

		return
	}

	if s.readOnly {
		writeError(w, http.StatusForbidden, "read_only", "API is in read-only mode", s.store.Revision())

		return
	}

	if username == "" || strings.Contains(username, "/") {
		writeError(w, http.StatusNotFound, "not_found", "unknown user", s.store.Revision())

		return
	}

	if err := s.store.Delete(username, r.Header.Get("If-Match")); err != nil {
		if errors.Is(err, users.ErrUserNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "unknown user", s.store.Revision())

			return
		}

		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), s.store.Revision())

		return
	}

	writeOK(w, http.StatusOK, username, s.store.Revision())
}

func revisionOrEmpty(s *Server) string {
	if s.store == nil {
		return ""
	}

	return s.store.Revision()
}

func readBody(r *http.Request, limit int64) ([]byte, error) {
	defer r.Body.Close() //nolint: errcheck

	reader := io.LimitReader(r.Body, limit+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("cannot read body: %w", err)
	}

	if int64(len(data)) > limit {
		return nil, fmt.Errorf("body exceeds limit")
	}

	return data, nil
}

func clientIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return nil
	}

	return net.ParseIP(host)
}

func makeWhitelist(cidrs []string) (cidranger.Ranger, error) {
	if len(cidrs) == 0 {
		return nil, nil
	}

	ranger := cidranger.NewPCTrieRanger()

	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("invalid whitelist CIDR %q: %w", cidr, err)
		}

		if err := ranger.Insert(cidranger.NewBasicRangerEntry(*network)); err != nil {
			return nil, fmt.Errorf("cannot insert CIDR %q: %w", cidr, err)
		}
	}

	return ranger, nil
}

func (s *Server) allowIP(ip net.IP) bool {
	if s.whitelist == nil || ip == nil {
		return true
	}

	ok, err := s.whitelist.Contains(ip)
	if err != nil {
		return false
	}

	return ok
}

func apiWhitelist(conf *config.Config) []string {
	if len(conf.API.Whitelist) > 0 {
		return conf.API.Whitelist
	}

	return []string{"127.0.0.1/32", "::1/128"}
}
