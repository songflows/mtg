package users

import (
	"fmt"
	"time"

	"github.com/9seconds/mtg/v2/mtglib"
	"github.com/pelletier/go-toml/v2"
)

// FileConfig is the on-disk users.toml schema (telemt-inspired).
type FileConfig struct {
	General struct {
		Links struct {
			PublicHost string `toml:"public-host"`
			PublicPort uint   `toml:"public-port"`
		} `toml:"links"`
		// DefaultHost is used when auto-generating ee-secrets for new users.
		DefaultHost string `toml:"default-host"`
	} `toml:"general"`
	Users []FileUser `toml:"users"`
}

// FileUser is one [[users]] table row.
type FileUser struct {
	Username            string `toml:"username"`
	Secret              string `toml:"secret,omitempty"`
	ExpirationRFC3339   string `toml:"expiration-rfc3339,omitempty"`
}

// User is a runtime user record.
type User struct {
	Username          string
	Secret            mtglib.Secret
	ExpiresAt         *time.Time
}

func (u *User) IsActive(now time.Time) bool {
	if u.ExpiresAt == nil {
		return true
	}

	return now.Before(*u.ExpiresAt)
}

func parseFileUser(fu FileUser, defaultHost string) (User, error) {
	if err := validateUsername(fu.Username); err != nil {
		return User{}, err
	}

	var expiresAt *time.Time

	if fu.ExpirationRFC3339 != "" {
		t, err := time.Parse(time.RFC3339, fu.ExpirationRFC3339)
		if err != nil {
			return User{}, fmt.Errorf("user %q: invalid expiration-rfc3339: %w", fu.Username, err)
		}

		expiresAt = &t
	}

	secretText := fu.Secret
	if secretText == "" {
		if defaultHost == "" {
			return User{}, fmt.Errorf("user %q: secret is empty and general.default-host is not set", fu.Username)
		}

		secretText = mtglib.GenerateSecret(defaultHost).String()
	}

	secret, err := mtglib.ParseSecret(secretText)
	if err != nil {
		return User{}, fmt.Errorf("user %q: %w", fu.Username, err)
	}

	return User{
		Username:  fu.Username,
		Secret:    secret,
		ExpiresAt: expiresAt,
	}, nil
}

func (fc *FileConfig) toUsers() ([]User, error) {
	defaultHost := fc.General.DefaultHost
	users := make([]User, 0, len(fc.Users))

	for _, fu := range fc.Users {
		u, err := parseFileUser(fu, defaultHost)
		if err != nil {
			return nil, err
		}

		users = append(users, u)
	}

	return users, nil
}

func (fc *FileConfig) fromUsers(users []User) {
	fc.Users = make([]FileUser, len(users))

	for i, u := range users {
		fu := FileUser{
			Username: u.Username,
			Secret:   u.Secret.String(),
		}

		if u.ExpiresAt != nil {
			fu.ExpirationRFC3339 = u.ExpiresAt.UTC().Format(time.RFC3339)
		}

		fc.Users[i] = fu
	}
}

func marshalFileConfig(fc *FileConfig) ([]byte, error) {
	return toml.Marshal(fc)
}

func unmarshalFileConfig(data []byte) (*FileConfig, error) {
	fc := &FileConfig{}

	if err := toml.Unmarshal(data, fc); err != nil {
		return nil, fmt.Errorf("cannot parse users toml: %w", err)
	}

	return fc, nil
}
