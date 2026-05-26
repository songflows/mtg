package users

import (
	"time"
)

// UserInfo is telemt-compatible API view.
type UserInfo struct {
	Username            string     `json:"username"`
	ExpirationRFC3339   *string    `json:"expiration_rfc3339,omitempty"`
	CurrentConnections  uint64     `json:"current_connections"`
	ActiveUniqueIPs     uint       `json:"active_unique_ips"`
	TotalOctets         uint64     `json:"total_octets"`
	Links               UserLinks  `json:"links"`
	Expired             bool       `json:"expired"`
}

// CreateUserResponse matches telemt create response.
type CreateUserResponse struct {
	User   UserInfo `json:"user"`
	Secret string   `json:"secret"`
}

// ToUserInfo builds API view for a user.
func ToUserInfo(u User, links LinkSettings, now time.Time) UserInfo {
	var expiration *string

	if u.ExpiresAt != nil {
		v := u.ExpiresAt.UTC().Format(time.RFC3339)
		expiration = &v
	}

	userLinks := BuildUserLinks(links, u.Secret, false)

	return UserInfo{
		Username:          u.Username,
		ExpirationRFC3339: expiration,
		Links:             userLinks,
		Expired:           !u.IsActive(now),
	}
}
