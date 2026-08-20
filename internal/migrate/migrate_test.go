package migrate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	appcrypto "github.com/atpx4869/hdhive_bot_go/internal/crypto"
	"github.com/atpx4869/hdhive_bot_go/internal/store"
)

func newTestDB(t *testing.T) *store.Store {
	t.Helper()
	cipher, err := appcrypto.New([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	dsn := "file:test-migrate-" + t.Name() + "?mode=memory&cache=shared"
	db, err := store.Open(context.Background(), dsn, cipher)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func writeJSON(t *testing.T, dir, name string, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestImportNormal(t *testing.T) {
	dir := t.TempDir()
	db := newTestDB(t)

	usersPath := writeJSON(t, dir, "users.json", userFile{
		AuthorizedUserIDs: []json.Number{json.Number("100"), json.Number("200")},
		Notes:             map[string]string{"100": "admin note"},
	})

	enabled := true
	p115Path := writeJSON(t, dir, "p115.json", map[string]legacyP115{
		"100": {Cookie: "UID=cookie100", TargetCID: "42", Enabled: &enabled},
	})

	result, err := Import(context.Background(), db, usersPath, p115Path)
	if err != nil {
		t.Fatal(err)
	}
	if result.Users != 2 {
		t.Fatalf("Users = %d, want 2", result.Users)
	}
	if result.Accounts != 1 {
		t.Fatalf("Accounts = %d, want 1", result.Accounts)
	}

	user, err := db.GetUser(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if !user.Authorized || user.Note != "admin note" {
		t.Fatalf("user 100: authorized=%v note=%q", user.Authorized, user.Note)
	}
}

func TestImportCorruptedJSON(t *testing.T) {
	dir := t.TempDir()
	db := newTestDB(t)
	badPath := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(badPath, []byte(`{invalid json`), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := Import(context.Background(), db, badPath, "")
	if err == nil {
		t.Fatal("expected error for corrupted JSON")
	}
}

func TestImportDuplicateIdempotent(t *testing.T) {
	dir := t.TempDir()
	db := newTestDB(t)
	usersPath := writeJSON(t, dir, "users.json", userFile{
		AuthorizedUserIDs: []json.Number{json.Number("100")},
		Notes:             map[string]string{"100": "first import"},
	})
	_, err := Import(context.Background(), db, usersPath, "")
	if err != nil {
		t.Fatal(err)
	}
	usersPath2 := writeJSON(t, dir, "users2.json", userFile{
		AuthorizedUserIDs: []json.Number{json.Number("100")},
		Notes:             map[string]string{"100": "second import"},
	})
	_, err = Import(context.Background(), db, usersPath2, "")
	if err != nil {
		t.Fatal(err)
	}
	user, err := db.GetUser(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if user.Note != "second import" {
		t.Fatalf("note = %q, want %q", user.Note, "second import")
	}
}

func TestImportEmptyFiles(t *testing.T) {
	dir := t.TempDir()
	db := newTestDB(t)
	usersPath := writeJSON(t, dir, "users.json", userFile{})
	p115Path := writeJSON(t, dir, "p115.json", map[string]legacyP115{})
	result, err := Import(context.Background(), db, usersPath, p115Path)
	if err != nil {
		t.Fatal(err)
	}
	if result.Users != 0 || result.Accounts != 0 {
		t.Fatalf("Users=%d Accounts=%d, want 0,0", result.Users, result.Accounts)
	}
}

func TestImportNotesOnly(t *testing.T) {
	dir := t.TempDir()
	db := newTestDB(t)
	usersPath := writeJSON(t, dir, "users.json", userFile{
		Notes: map[string]string{"300": "only note"},
	})
	result, err := Import(context.Background(), db, usersPath, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Users != 1 {
		t.Fatalf("Users = %d, want 1", result.Users)
	}
	user, err := db.GetUser(context.Background(), 300)
	if err != nil {
		t.Fatal(err)
	}
	if user.Authorized {
		t.Fatal("notes-only user should not be authorized")
	}
}
