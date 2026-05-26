package users

import (
	"fmt"
	"regexp"
	"unicode/utf8"
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func validateUsername(username string) error {
	if username == "" {
		return fmt.Errorf("username is required")
	}

	if l := utf8.RuneCountInString(username); l < 1 || l > 64 {
		return fmt.Errorf("username length must be 1..64")
	}

	if !usernamePattern.MatchString(username) {
		return fmt.Errorf("username must match [A-Za-z0-9_.-]")
	}

	return nil
}
