package store

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/term"
)

// StorageKeyEnv is the environment variable the storage passphrase is read
// from (ADR 0020). When it is set, no prompt is shown; scripted use sets it.
const StorageKeyEnv = "VALISS_STORAGE_KEY"

// ErrNoPassphrase reports that no passphrase was available: the environment
// variable is unset and there is no terminal to prompt on.
var ErrNoPassphrase = errors.New("valiss: no storage passphrase: set " + StorageKeyEnv + " or run interactively")

// ErrEmptyPassphrase reports that the storage passphrase resolved to empty. An
// empty passphrase would derive a store key from nothing, leaving the store
// effectively unprotected, so it is refused on every path (the environment
// variable as well as the interactive prompt).
var ErrEmptyPassphrase = errors.New("valiss: storage passphrase must not be empty")

// Passphrase resolves the store passphrase (ADR 0020): the VALISS_STORAGE_KEY
// environment variable when set, otherwise an interactive hidden prompt on the
// controlling terminal. The passphrase is never written to disk; it is fed to
// the store's key-derivation step and discarded.
//
// In a non-interactive context with the variable unset, it fails with
// ErrNoPassphrase rather than blocking on a prompt that cannot be answered.
func Passphrase() ([]byte, error) {
	if v, ok := os.LookupEnv(StorageKeyEnv); ok {
		if v == "" {
			return nil, ErrEmptyPassphrase
		}
		return []byte(v), nil
	}
	return promptPassphrase("Storage passphrase: ")
}

// PassphraseConfirmed resolves the passphrase like Passphrase, but when it
// falls through to an interactive prompt it asks twice and requires the two
// entries to match. It is used where a mistyped passphrase would be
// unrecoverable, such as initializing a new store.
func PassphraseConfirmed() ([]byte, error) {
	if v, ok := os.LookupEnv(StorageKeyEnv); ok {
		if v == "" {
			return nil, ErrEmptyPassphrase
		}
		return []byte(v), nil
	}
	first, err := promptPassphrase("New storage passphrase: ")
	if err != nil {
		return nil, err
	}
	if len(first) == 0 {
		return nil, ErrEmptyPassphrase
	}
	again, err := promptPassphrase("Confirm storage passphrase: ")
	if err != nil {
		return nil, err
	}
	if string(first) != string(again) {
		return nil, errors.New("valiss: passphrases do not match")
	}
	return first, nil
}

// promptPassphrase reads a line of hidden input from the controlling terminal.
func promptPassphrase(prompt string) ([]byte, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil, ErrNoPassphrase
	}
	fmt.Fprint(os.Stderr, prompt)
	pw, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("valiss: reading passphrase: %w", err)
	}
	return pw, nil
}
