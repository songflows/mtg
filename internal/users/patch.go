package users

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/9seconds/mtg/v2/mtglib"
)

// PatchUserRequest is a telemt-compatible partial update body.
// Omitted fields are left unchanged. expiration_rfc3339 / expires_at may be
// JSON null to remove the expiration.
type PatchUserRequest struct {
	Secret            json.RawMessage `json:"secret,omitempty"`
	ExpirationRFC3339 json.RawMessage `json:"expiration_rfc3339,omitempty"`
	ExpiresAt         json.RawMessage `json:"expires_at,omitempty"`
	MaxUniqueIPs      json.RawMessage `json:"max_unique_ips,omitempty"`
	ActiveUniqueIPs   json.RawMessage `json:"active_unique_ips,omitempty"`
}

func (req PatchUserRequest) applyTo(u User) (User, error) {
	updated := u

	if len(req.Secret) > 0 {
		secret, err := parsePatchSecret(req.Secret)
		if err != nil {
			return User{}, err
		}

		updated.Secret = secret
	}

	expRaw := req.ExpirationRFC3339
	if len(req.ExpiresAt) > 0 {
		expRaw = req.ExpiresAt
	}

	if len(expRaw) > 0 {
		expiresAt, err := parsePatchExpiration(expRaw)
		if err != nil {
			return User{}, err
		}

		updated.ExpiresAt = expiresAt
	}

	maxRaw := req.MaxUniqueIPs
	if len(req.ActiveUniqueIPs) > 0 {
		if len(maxRaw) > 0 &&
			!bytes.Equal(bytes.TrimSpace(maxRaw), bytes.TrimSpace(req.ActiveUniqueIPs)) {
			return User{}, fmt.Errorf("max_unique_ips and active_unique_ips conflict")
		}

		maxRaw = req.ActiveUniqueIPs
	}

	if len(maxRaw) > 0 {
		maxIPs, err := parsePatchMaxUniqueIPs(maxRaw)
		if err != nil {
			return User{}, err
		}

		updated.MaxUniqueIPs = maxIPs
	}

	if !updated.Secret.Valid() {
		return User{}, fmt.Errorf("user %q: secret is invalid", u.Username)
	}

	return updated, nil
}

func parsePatchSecret(raw json.RawMessage) (mtglib.Secret, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return mtglib.Secret{}, fmt.Errorf("secret cannot be null")
	}

	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return mtglib.Secret{}, fmt.Errorf("secret must be a string: %w", err)
	}

	if text == "" {
		return mtglib.Secret{}, fmt.Errorf("secret cannot be empty")
	}

	secret, err := mtglib.ParseSecret(text)
	if err != nil {
		return mtglib.Secret{}, err
	}

	return secret, nil
}

func parsePatchMaxUniqueIPs(raw json.RawMessage) (*int, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}

	var value int
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("max_unique_ips must be a number or null: %w", err)
	}

	if value < 0 {
		return nil, fmt.Errorf("max_unique_ips must be >= 0")
	}

	if value == 0 {
		v := 0
		return &v, nil
	}

	return &value, nil
}

func parsePatchExpiration(raw json.RawMessage) (*time.Time, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}

	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return nil, fmt.Errorf("expiration must be a string or null: %w", err)
	}

	if text == "" {
		return nil, nil
	}

	t, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return nil, fmt.Errorf("invalid expiration timestamp: %w", err)
	}

	return &t, nil
}
