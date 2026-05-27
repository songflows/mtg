package mtglib

import "net"

// UserConnectionLimiter enforces per-user connection limits (e.g. max unique IPs).
type UserConnectionLimiter interface {
	Acquire(secret Secret, ip net.IP) error
	Release(secret Secret, ip net.IP)
}
