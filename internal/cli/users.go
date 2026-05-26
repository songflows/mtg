package cli

import (
	"fmt"
	"path/filepath"

	"github.com/9seconds/mtg/v2/internal/config"
	"github.com/9seconds/mtg/v2/internal/users"
)

func makeUsersStore(conf *config.Config, configPath string) (*users.Store, error) {
	if conf.UsersFile == "" {
		return nil, nil
	}

	path := conf.UsersFile
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(configPath), path)
	}

	store, err := users.NewStore(path)
	if err != nil {
		return nil, fmt.Errorf("cannot load users file: %w", err)
	}

	if store.DefaultHost() == "" && conf.Secret.Valid() {
		store.SetDefaultHost(conf.Secret.Host)
	}

	return store, nil
}
