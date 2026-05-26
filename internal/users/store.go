package users

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/9seconds/mtg/v2/mtglib"
)

var (
	ErrUserExists   = errors.New("user already exists")
	ErrUserNotFound = errors.New("user not found")
	ErrUserExpired  = errors.New("user expired")
)

// Store persists users in a TOML file and serves active secrets to the proxy.
type Store struct {
	mu sync.RWMutex

	path     string
	file     FileConfig
	users    map[string]User
	revision string
}

// NewStore loads or creates a users file.
func NewStore(path string) (*Store, error) {
	s := &Store{
		path:  path,
		users: map[string]User{},
	}

	if err := s.reload(); err != nil {
		return nil, err
	}

	return s, nil
}

// Path returns the backing file path.
func (s *Store) Path() string {
	return s.path
}

// Revision returns SHA-256 hex of the current file bytes (telemt-style).
func (s *Store) Revision() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.revision
}

// LinkSettings returns link generation settings from file config.
func (s *Store) LinkSettings() LinkSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return LinkSettings{
		PublicHost: s.file.General.Links.PublicHost,
		PublicPort: s.file.General.Links.PublicPort,
	}
}

// DefaultHost returns hostname for auto-generated secrets.
func (s *Store) DefaultHost() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.file.General.DefaultHost
}

// SetDefaultHost updates default host used for generated secrets.
func (s *Store) SetDefaultHost(host string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.file.General.DefaultHost = host
}

// SetLinkSettings updates public link endpoint settings.
func (s *Store) SetLinkSettings(settings LinkSettings) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.file.General.Links.PublicHost = settings.PublicHost
	s.file.General.Links.PublicPort = settings.PublicPort
}

// List returns all users (including expired).
func (s *Store) List() []User {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]User, 0, len(s.users))
	for _, u := range s.users {
		out = append(out, u)
	}

	return out
}

// Get returns a user by username.
func (s *Store) Get(username string) (User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	u, ok := s.users[username]
	if !ok {
		return User{}, ErrUserNotFound
	}

	return u, nil
}

// CreateUserRequest is telemt-compatible create body (+ expires_at alias).
type CreateUserRequest struct {
	Username            string  `json:"username"`
	Secret              string  `json:"secret,omitempty"`
	ExpirationRFC3339   *string `json:"expiration_rfc3339,omitempty"`
	ExpiresAt           *string `json:"expires_at,omitempty"`
}

// Create adds a new user and persists the file.
func (s *Store) Create(req CreateUserRequest, expectedRevision string) (User, error) {
	if err := validateUsername(req.Username); err != nil {
		return User{}, err
	}

	expiration, err := parseExpiration(req.ExpirationRFC3339, req.ExpiresAt)
	if err != nil {
		return User{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.checkRevision(expectedRevision); err != nil {
		return User{}, err
	}

	if _, ok := s.users[req.Username]; ok {
		return User{}, ErrUserExists
	}

	fu := FileUser{
		Username:          req.Username,
		Secret:              req.Secret,
		ExpirationRFC3339: formatExpiration(expiration),
	}

	u, err := parseFileUser(fu, s.file.General.DefaultHost)
	if err != nil {
		return User{}, err
	}

	s.users[u.Username] = u

	if err := s.persistLocked(); err != nil {
		delete(s.users, u.Username)

		return User{}, err
	}

	return u, nil
}

// Delete removes a user permanently (revoke).
func (s *Store) Delete(username string, expectedRevision string) error {
	if err := validateUsername(username); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.checkRevision(expectedRevision); err != nil {
		return err
	}

	if _, ok := s.users[username]; !ok {
		return ErrUserNotFound
	}

	delete(s.users, username)

	return s.persistLocked()
}

// Reload re-reads the file from disk (hot reload after external edit).
func (s *Store) Reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.reloadLocked()
}

func (s *Store) reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.reloadLocked()
}

func (s *Store) reloadLocked() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.file = FileConfig{}
			s.users = map[string]User{}
			s.revision = hashBytes(nil)

			return nil
		}

		return fmt.Errorf("cannot read users file: %w", err)
	}

	fc, err := unmarshalFileConfig(data)
	if err != nil {
		return err
	}

	users, err := fc.toUsers()
	if err != nil {
		return err
	}

	index := make(map[string]User, len(users))
	for _, u := range users {
		if _, dup := index[u.Username]; dup {
			return fmt.Errorf("duplicate username %q", u.Username)
		}

		index[u.Username] = u
	}

	s.file = *fc
	s.users = index
	s.revision = hashBytes(data)

	return nil
}

func (s *Store) persistLocked() error {
	s.file.fromUsers(s.sortedUsersLocked())

	data, err := marshalFileConfig(&s.file)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("cannot create users directory: %w", err)
	}

	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return fmt.Errorf("cannot write users file: %w", err)
	}

	s.revision = hashBytes(data)

	return nil
}

func (s *Store) sortedUsersLocked() []User {
	out := make([]User, 0, len(s.users))
	for _, u := range s.users {
		out = append(out, u)
	}

	// stable sort by username
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Username < out[i].Username {
				out[i], out[j] = out[j], out[i]
			}
		}
	}

	return out
}

func (s *Store) checkRevision(expected string) error {
	if expected == "" || expected == s.revision {
		return nil
	}

	return fmt.Errorf("revision conflict: expected %s, have %s", expected, s.revision)
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:])
}

func parseExpiration(primary, alias *string) (*time.Time, error) {
	var raw *string

	switch {
	case primary != nil:
		raw = primary
	case alias != nil:
		raw = alias
	default:
		return nil, nil
	}

	if *raw == "" {
		return nil, nil
	}

	t, err := time.Parse(time.RFC3339, *raw)
	if err != nil {
		return nil, fmt.Errorf("invalid expiration timestamp: %w", err)
	}

	return &t, nil
}

// ActiveSecrets implements mtglib.SecretProvider.
func (s *Store) ActiveSecrets(now time.Time) []mtglib.Secret {
	return s.activeSecretsAt(now)
}

func (s *Store) activeSecretsAt(now time.Time) []mtglib.Secret {
	s.mu.RLock()
	defer s.mu.RUnlock()

	secrets := make([]mtglib.Secret, 0, len(s.users))

	for _, u := range s.users {
		if u.IsActive(now) {
			secrets = append(secrets, u.Secret)
		}
	}

	return secrets
}

func formatExpiration(t *time.Time) string {
	if t == nil {
		return ""
	}

	return t.UTC().Format(time.RFC3339)
}
