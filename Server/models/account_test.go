package models

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestAccountStorePersistsSecurelyAndConcurrently(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)
	if err := LoadAccounts(); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			account := Account{ID: fmt.Sprint(index), Email: fmt.Sprintf("user%d@example.com", index), Key: "secret", Name: "test"}
			if err := UpsertAccount(account); err != nil {
				t.Errorf("UpsertAccount(%d): %v", index, err)
			}
		}(i)
	}
	wg.Wait()

	if got := len(ListAccounts()); got != 20 {
		t.Fatalf("got %d accounts, want 20", got)
	}
	path := filepath.Join(dataDir, "accounts.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("accounts permissions are %o, want 600", info.Mode().Perm())
	}

	copyOfAccounts := ListAccounts()
	copyOfAccounts[0].Name = "mutated"
	stored, ok := FindAccount(copyOfAccounts[0].ID)
	if !ok || stored.Name == "mutated" {
		t.Fatal("ListAccounts exposed mutable store state")
	}

	deleted, err := DeleteAccount("0")
	if err != nil || !deleted {
		t.Fatalf("DeleteAccount returned deleted=%v err=%v", deleted, err)
	}
	if err := LoadAccounts(); err != nil {
		t.Fatal(err)
	}
	if _, ok := FindAccount("0"); ok {
		t.Fatal("deleted account reappeared after reload")
	}
}
