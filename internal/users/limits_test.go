package users_test

import (
	"net"
	"testing"

	"github.com/9seconds/mtg/v2/internal/users"
	"github.com/stretchr/testify/require"
)

func TestMaxUniqueIPsLimit(t *testing.T) {
	store, err := users.NewStore(t.TempDir() + "/users.toml")
	require.NoError(t, err)
	store.SetDefaultHost("google.com")

	maxIPs := 1
	_, err = store.Create(users.CreateUserRequest{
		Username:     "alice",
		MaxUniqueIPs: &maxIPs,
	}, "")
	require.NoError(t, err)

	alice, err := store.Get("alice")
	require.NoError(t, err)

	ip1 := net.ParseIP("203.0.113.1")
	ip2 := net.ParseIP("203.0.113.2")

	require.NoError(t, store.Acquire(alice.Secret, ip1))
	require.ErrorIs(t, store.Acquire(alice.Secret, ip2), users.ErrMaxUniqueIPsExceeded)

	store.Release(alice.Secret, ip1)
	require.NoError(t, store.Acquire(alice.Secret, ip2))

	active, conns := store.ConnectionStatsForTest("alice")
	require.Equal(t, 1, active)
	require.Equal(t, 1, conns)
}

func TestDeleteClearsConnectionStats(t *testing.T) {
	store, err := users.NewStore(t.TempDir() + "/users.toml")
	require.NoError(t, err)
	store.SetDefaultHost("google.com")

	_, err = store.Create(users.CreateUserRequest{Username: "alice"}, "")
	require.NoError(t, err)

	alice, err := store.Get("alice")
	require.NoError(t, err)

	require.NoError(t, store.Acquire(alice.Secret, net.ParseIP("203.0.113.1")))
	require.NoError(t, store.Acquire(alice.Secret, net.ParseIP("203.0.113.1")))
	require.NoError(t, store.Acquire(alice.Secret, net.ParseIP("203.0.113.1")))

	active, conns := store.ConnectionStatsForTest("alice")
	require.Equal(t, 1, active)
	require.Equal(t, 3, conns)

	require.NoError(t, store.Delete("alice", store.Revision()))

	_, err = store.Create(users.CreateUserRequest{Username: "alice"}, "")
	require.NoError(t, err)

	active, conns = store.ConnectionStatsForTest("alice")
	require.Equal(t, 0, active)
	require.Equal(t, 0, conns)
}
