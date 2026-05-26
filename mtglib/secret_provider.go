package mtglib

import "time"

// SecretProvider supplies active proxy secrets (multi-user mode).
type SecretProvider interface {
	ActiveSecrets(now time.Time) []Secret
}
