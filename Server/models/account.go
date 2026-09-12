package models

import (
	"cloudflare-tools/server/config"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type Account struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Key   string `json:"key"`
	Name  string `json:"name"`
}

var (
	accounts   []Account
	accountsMu sync.RWMutex
)

func LoadAccounts() error {
	path := config.DataPath("accounts.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			accountsMu.Lock()
			accounts = nil
			accountsMu.Unlock()
			return nil
		}
		return fmt.Errorf("read accounts: %w", err)
	}

	var loaded []Account
	if err := json.Unmarshal(data, &loaded); err != nil {
		return fmt.Errorf("parse accounts: %w", err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		return fmt.Errorf("secure accounts file: %w", err)
	}

	accountsMu.Lock()
	accounts = loaded
	accountsMu.Unlock()
	return nil
}

func ListAccounts() []Account {
	accountsMu.RLock()
	defer accountsMu.RUnlock()
	return append([]Account(nil), accounts...)
}

func FindAccount(id string) (Account, bool) {
	accountsMu.RLock()
	defer accountsMu.RUnlock()
	for _, account := range accounts {
		if account.ID == id {
			return account, true
		}
	}
	return Account{}, false
}

func UpsertAccount(account Account) error {
	accountsMu.Lock()
	defer accountsMu.Unlock()

	next := append([]Account(nil), accounts...)
	found := false
	for i := range next {
		if next[i].ID == account.ID {
			next[i] = account
			found = true
			break
		}
	}
	if !found {
		next = append(next, account)
	}
	if err := saveAccounts(next); err != nil {
		return err
	}
	accounts = next
	return nil
}

// UpdateAccount changes one existing account while keeping its saved key when
// the edit form leaves the replacement key empty. The read and write happen
// under one lock so a concurrent edit cannot restore a stale key.
func UpdateAccount(id, name, email, key string) (Account, bool, error) {
	accountsMu.Lock()
	defer accountsMu.Unlock()

	next := append([]Account(nil), accounts...)
	for i := range next {
		if next[i].ID != id {
			continue
		}
		next[i].Name = name
		next[i].Email = email
		if key != "" {
			next[i].Key = key
		}
		if err := saveAccounts(next); err != nil {
			return Account{}, true, err
		}
		accounts = next
		return next[i], true, nil
	}
	return Account{}, false, nil
}

func DeleteAccount(id string) (bool, error) {
	accountsMu.Lock()
	defer accountsMu.Unlock()

	next := make([]Account, 0, len(accounts))
	found := false
	for _, account := range accounts {
		if account.ID == id {
			found = true
			continue
		}
		next = append(next, account)
	}
	if !found {
		return false, nil
	}
	if err := saveAccounts(next); err != nil {
		return false, err
	}
	accounts = next
	return true, nil
}

func saveAccounts(next []Account) error {
	path := config.DataPath("accounts.json")
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}

	temp, err := os.CreateTemp(dir, ".accounts-*.json")
	if err != nil {
		return fmt.Errorf("create accounts temporary file: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return fmt.Errorf("secure accounts temporary file: %w", err)
	}
	encoder := json.NewEncoder(temp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(next); err != nil {
		temp.Close()
		return fmt.Errorf("encode accounts: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("sync accounts: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close accounts: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace accounts: %w", err)
	}

	dirHandle, err := os.Open(dir)
	if err == nil {
		_ = dirHandle.Sync()
		_ = dirHandle.Close()
	}
	return nil
}
