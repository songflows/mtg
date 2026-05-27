package users

import (
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/9seconds/mtg/v2/mtglib"
)

// ErrMaxUniqueIPsExceeded is returned when a user already has connections from
// the maximum number of distinct source IPs.
var ErrMaxUniqueIPsExceeded = errors.New("max unique ips exceeded")

type connectionLimiter struct {
	mu    sync.Mutex
	store *Store
	// username -> source IP -> active connection count
	byUser map[string]map[string]int
}

func newConnectionLimiter(store *Store) *connectionLimiter {
	return &connectionLimiter{
		store:  store,
		byUser: map[string]map[string]int{},
	}
}

// Acquire registers a connection for the user that owns secret.
func (l *connectionLimiter) Acquire(secret mtglib.Secret, ip net.IP) error {
	user, ok := l.store.userBySecret(secret)
	if !ok {
		return nil
	}

	if user.MaxUniqueIPs == nil {
		l.mu.Lock()
		l.incLocked(user.Username, ipKey(ip))
		l.mu.Unlock()

		return nil
	}

	max := *user.MaxUniqueIPs
	if max == 0 {
		return ErrMaxUniqueIPsExceeded
	}

	ipStr := ipKey(ip)

	l.mu.Lock()
	defer l.mu.Unlock()

	ips := l.byUser[user.Username]
	if ips == nil {
		ips = map[string]int{}
		l.byUser[user.Username] = ips
	}

	if ips[ipStr] > 0 {
		ips[ipStr]++

		return nil
	}

	if len(ips) >= max {
		return fmt.Errorf("%w (%d)", ErrMaxUniqueIPsExceeded, max)
	}

	ips[ipStr] = 1

	return nil
}

// Release drops one active connection for the user/IP pair.
func (l *connectionLimiter) Release(secret mtglib.Secret, ip net.IP) {
	user, ok := l.store.userBySecret(secret)
	if !ok {
		return
	}

	ipStr := ipKey(ip)

	l.mu.Lock()
	defer l.mu.Unlock()

	ips := l.byUser[user.Username]
	if ips == nil {
		return
	}

	ips[ipStr]--
	if ips[ipStr] <= 0 {
		delete(ips, ipStr)
	}

	if len(ips) == 0 {
		delete(l.byUser, user.Username)
	}
}

// clearUser drops all in-memory connection counters for a user (e.g. after DELETE).
func (l *connectionLimiter) clearUser(username string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.byUser, username)
}

// Stats returns live unique IP count and total connections for a user.
func (l *connectionLimiter) Stats(username string) (activeUniqueIPs, currentConnections int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	ips := l.byUser[username]
	activeUniqueIPs = len(ips)

	for _, n := range ips {
		currentConnections += n
	}

	return activeUniqueIPs, currentConnections
}

func (l *connectionLimiter) incLocked(username, ipStr string) {
	ips := l.byUser[username]
	if ips == nil {
		ips = map[string]int{}
		l.byUser[username] = ips
	}

	ips[ipStr]++
}

func ipKey(ip net.IP) string {
	if ip4 := ip.To4(); ip4 != nil {
		return ip4.String()
	}

	return ip.String()
}
