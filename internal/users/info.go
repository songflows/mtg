package users

import (
	"time"
)

// UserInfo is telemt-compatible API view.
type UserInfo struct {
	Username            string    `json:"username"`
	MaxUniqueIPs        *int      `json:"max_unique_ips,omitempty"`
	ExpirationRFC3339   *string   `json:"expiration_rfc3339,omitempty"`
	CurrentConnections  uint64    `json:"current_connections"`
	ActiveUniqueIPs     uint      `json:"active_unique_ips"`
	TotalOctets         uint64    `json:"total_octets"`
	Links               UserLinks `json:"links"`
	Expired             bool      `json:"expired"`
}

// CreateUserResponse matches telemt create response.
type CreateUserResponse struct {
	User   UserInfo `json:"user"`
	Secret string   `json:"secret"`
}

// ToUserInfo builds API view for a user.
func ToUserInfo(u User, links LinkSettings, now time.Time, store *Store) UserInfo {
	var expiration *string

	if u.ExpiresAt != nil {
		v := u.ExpiresAt.UTC().Format(time.RFC3339)
		expiration = &v
	}

	userLinks := BuildUserLinks(links, u.Secret, true)

	info := UserInfo{
		Username:          u.Username,
		MaxUniqueIPs:      u.MaxUniqueIPs,
		ExpirationRFC3339: expiration,
		Links:             userLinks,
		Expired:           !u.IsActive(now),
	}

	if store != nil && store.limiter != nil {
		active, conns := store.limiter.Stats(u.Username)
		info.ActiveUniqueIPs = uint(active)
		info.CurrentConnections = uint64(conns)
	}

	return info
}
