package users_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/9seconds/mtg/v2/internal/users"
	"github.com/stretchr/testify/suite"
)

type StoreTestSuite struct {
	suite.Suite

	dir  string
	path string
}

func (s *StoreTestSuite) SetupTest() {
	s.dir = s.T().TempDir()
	s.path = filepath.Join(s.dir, "users.toml")
}

func (s *StoreTestSuite) TestCreateGetDelete() {
	store, err := users.NewStore(s.path)
	s.Require().NoError(err)

	store.SetDefaultHost("google.com")

	exp := "2030-01-02T15:04:05Z"
	created, err := store.Create(users.CreateUserRequest{
		Username:          "alice",
		ExpirationRFC3339: &exp,
	}, "")
	s.Require().NoError(err)
	s.NotEmpty(created.Secret.String())

	got, err := store.Get("alice")
	s.Require().NoError(err)
	s.Equal("alice", got.Username)

	_, err = store.Create(users.CreateUserRequest{Username: "alice"}, "")
	s.ErrorIs(err, users.ErrUserExists)

	s.Require().NoError(store.Delete("alice", store.Revision()))
	_, err = store.Get("alice")
	s.ErrorIs(err, users.ErrUserNotFound)
}

func (s *StoreTestSuite) TestExpiresAtAlias() {
	store, err := users.NewStore(s.path)
	s.Require().NoError(err)
	store.SetDefaultHost("google.com")

	exp := "2030-06-01T00:00:00Z"
	_, err = store.Create(users.CreateUserRequest{
		Username:  "bob",
		ExpiresAt: &exp,
	}, "")
	s.Require().NoError(err)

	u, err := store.Get("bob")
	s.Require().NoError(err)
	s.Require().NotNil(u.ExpiresAt)
}

func (s *StoreTestSuite) TestActiveSecretsSkipsExpired() {
	content := []byte(`
[general]
default-host = "google.com"

[[users]]
username = "alive"
secret = "ee473ce5d4958eb5f968c87680a23854a0676f6f676c652e636f6d"

[[users]]
username = "dead"
secret = "ee367a189aee18fa31c190054efd4a8e9573746f726167652e676f6f676c65617069732e636f6d"
expiration-rfc3339 = "2000-01-01T00:00:00Z"
`)
	s.Require().NoError(os.WriteFile(s.path, content, 0o600))

	store, err := users.NewStore(s.path)
	s.Require().NoError(err)

	secrets := store.ActiveSecrets(time.Now())
	s.Len(secrets, 1)
}

func TestStore(t *testing.T) {
	suite.Run(t, new(StoreTestSuite))
}
