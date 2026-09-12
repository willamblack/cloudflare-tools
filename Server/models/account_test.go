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

func TestUpdateAccountPreservesOrReplacesKey(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	if err := LoadAccounts(); err != nil {
		t.Fatal(err)
	}
	if err := UpsertAccount(Account{ID: "one", Name: "old", Email: "old@example.com", Key: "old-secret"}); err != nil {
		t.Fatal(err)
	}

	updated, found, err := UpdateAccount("one", "new", "new@example.com", "")
	if err != nil || !found || updated.Key != "old-secret" {
		t.Fatalf("blank key should preserve existing secret: %#v, %v, %v", updated, found, err)
	}
	updated, found, err = UpdateAccount("one", "newer", "newer@example.com", "new-secret")
	if err != nil || !found || updated.Key != "new-secret" {
		t.Fatalf("nonblank key should replace secret: %#v, %v, %v", updated, found, err)
	}
	if _, found, err := UpdateAccount("missing", "x", "x@example.com", ""); found || err != nil {
		t.Fatalf("missing account returned found=%v err=%v", found, err)
	}
	if err := LoadAccounts(); err != nil {
		t.Fatal(err)
	}
	stored, ok := FindAccount("one")
	if !ok || stored.Name != "newer" || stored.Email != "newer@example.com" || stored.Key != "new-secret" {
		t.Fatalf("updated account did not survive reload: %#v", stored)
	}
}
