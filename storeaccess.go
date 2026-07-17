package main

import (
	"errors"
	"os"
	"sort"
	"strings"

	"github.com/spf13/viper"

	"valiss.dev/cli/valiss/internal/store"
)

// storeDir resolves the store directory: the store-dir configuration key when
// set (bindable from config or the VALISS_STORE_DIR environment variable),
// otherwise the ~/.valiss/store default (ADR 0017, 0020).
func storeDir() (string, error) {
	if dir := viper.GetString("store-dir"); dir != "" {
		return dir, nil
	}
	return store.DefaultDir()
}

// openStore opens an existing operator store, resolving the storage passphrase
// from VALISS_STORAGE_KEY or an interactive prompt (ADR 0020).
func openStore(operator string) (*store.Local, error) {
	dir, err := storeDir()
	if err != nil {
		return nil, err
	}
	pass, err := store.Passphrase()
	if err != nil {
		return nil, err
	}
	return store.Open(dir, operator, pass)
}

// initStore creates a new operator store, resolving the passphrase with
// confirmation (a mistyped passphrase on a new store would be unrecoverable).
func initStore(operator string, cfg store.Config) (*store.Local, error) {
	dir, err := storeDir()
	if err != nil {
		return nil, err
	}
	pass, err := store.PassphraseConfirmed()
	if err != nil {
		return nil, err
	}
	return store.Init(dir, operator, pass, cfg)
}

// openOrInitStore opens an operator's store, creating it with default
// configuration if it does not exist yet. It is the ergonomic path for
// 'operator add', which should just work without a preceding 'store init'.
func openOrInitStore(operator string) (*store.Local, error) {
	st, err := openStore(operator)
	if err == nil {
		return st, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	return initStore(operator, store.Config{AuditRetention: defaultAuditRetention})
}

// listStoreOperators returns the operator names that have a store under dir,
// sorted. An absent store directory is not an error: it lists nothing.
func listStoreOperators(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if name, ok := strings.CutSuffix(e.Name(), ".db"); ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}
