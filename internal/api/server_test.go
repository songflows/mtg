package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/9seconds/mtg/v2/internal/api"
	"github.com/9seconds/mtg/v2/internal/config"
	"github.com/9seconds/mtg/v2/internal/users"
	"github.com/9seconds/mtg/v2/mtglib"
	"github.com/stretchr/testify/suite"
)

type APITestSuite struct {
	suite.Suite

	store  *users.Store
	server *api.Server
}

func (s *APITestSuite) SetupTest() {
	path := s.T().TempDir() + "/users.toml"
	store, err := users.NewStore(path)
	s.Require().NoError(err)
	store.SetDefaultHost("google.com")
	store.SetLinkSettings(users.LinkSettings{PublicHost: "1.2.3.4", PublicPort: 443})
	s.store = store

	conf := &config.Config{}
	conf.Secret = mtglib.GenerateSecret("google.com")
	conf.BindTo.Set("0.0.0.0:443")
	conf.API.Enabled.Value = true
	conf.API.Listen.Set("127.0.0.1:9091")

	srv, err := api.NewServer(conf, store)
	s.Require().NoError(err)
	s.server = srv
}

func (s *APITestSuite) do(method, path string, body any) *httptest.ResponseRecorder {
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		s.Require().NoError(err)
	}

	req := httptest.NewRequest(method, path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:12345"

	rec := httptest.NewRecorder()
	s.server.Handler().ServeHTTP(rec, req)

	return rec
}

func (s *APITestSuite) TestCreateGetDeleteUser() {
	exp := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)

	rec := s.do(http.MethodPost, "/v1/users", map[string]any{
		"username":             "alice",
		"expiration_rfc3339": exp,
	})
	s.Equal(http.StatusCreated, rec.Code)

	rec = s.do(http.MethodGet, "/v1/users/alice", nil)
	s.Equal(http.StatusOK, rec.Code)

	newExp := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	rec = s.do(http.MethodPatch, "/v1/users/alice", map[string]any{
		"expires_at": newExp,
	})
	s.Equal(http.StatusOK, rec.Code)

	var patchEnvelope struct {
		OK   bool `json:"ok"`
		Data struct {
			ExpirationRFC3339 string `json:"expiration_rfc3339"`
		} `json:"data"`
	}
	s.Require().NoError(json.Unmarshal(rec.Body.Bytes(), &patchEnvelope))
	s.True(patchEnvelope.OK)
	s.Equal(newExp, patchEnvelope.Data.ExpirationRFC3339)

	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Username string `json:"username"`
			Links    struct {
				TLS []string `json:"tls"`
			} `json:"links"`
		} `json:"data"`
	}
	s.Require().NoError(json.Unmarshal(rec.Body.Bytes(), &envelope))
	s.True(envelope.OK)
	s.Equal("alice", envelope.Data.Username)
	s.NotEmpty(envelope.Data.Links.TLS)

	rec = s.do(http.MethodDelete, "/v1/users/alice", nil)
	s.Equal(http.StatusOK, rec.Code)

	rec = s.do(http.MethodGet, "/v1/users/alice", nil)
	s.Equal(http.StatusNotFound, rec.Code)
}

func TestAPI(t *testing.T) {
	suite.Run(t, new(APITestSuite))
}
