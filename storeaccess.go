package main

import (
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
