package perm

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func store(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "operators.json")
	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return s, path
}

// TestMissingFileIsEmpty checks a fresh server starts with no operators rather
// than refusing to start.
func TestMissingFileIsEmpty(t *testing.T) {
	s, _ := store(t)
	if names := s.Names(); len(names) != 0 {
		t.Fatalf("new store has operators: %v", names)
	}
	if s.IsOperator("anyone") {
		t.Error("new store reports an operator")
	}
}

// TestEmptyFileIsEmpty checks a zero-byte file is treated as an empty list.
// json.Unmarshal rejects empty input, so this needs handling of its own.
func TestEmptyFileIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operators.json")
	if err := os.WriteFile(path, []byte("  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load on empty file: %v", err)
	}
	if len(s.Names()) != 0 {
		t.Error("empty file produced operators")
	}
}

// TestAddPersists checks an operator survives a reload, which is the whole
// point of writing the file.
func TestAddPersists(t *testing.T) {
	s, path := store(t)
	added, err := s.Add("Steve", "2535400000000000")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !added {
		t.Error("Add reported Steve was already an operator")
	}

	again, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !again.IsOperator("Steve") {
		t.Error("Steve did not survive a reload")
	}

	// The XUID is recorded so an entry stays traceable after a rename.
	var list []Operator
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatalf("file is not valid JSON: %v", err)
	}
	if len(list) != 1 || list[0].XUID != "2535400000000000" {
		t.Errorf("stored entry is %+v", list)
	}
}

// TestLookupIgnoresCase checks names match the way players type them.
func TestLookupIgnoresCase(t *testing.T) {
	s, _ := store(t)
	if _, err := s.Add("Steve", ""); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"steve", "STEVE", "StEvE"} {
		if !s.IsOperator(name) {
			t.Errorf("IsOperator(%q) = false", name)
		}
	}
	if err := s.Remove("sTeVe"); err != nil {
		t.Errorf("Remove with different case: %v", err)
	}
}

// TestAddTwiceIsNotAnError checks re-opping refreshes rather than failing.
func TestAddTwiceIsNotAnError(t *testing.T) {
	s, _ := store(t)
	if _, err := s.Add("Alex", "old"); err != nil {
		t.Fatal(err)
	}
	added, err := s.Add("Alex", "new")
	if err != nil {
		t.Fatalf("second Add: %v", err)
	}
	if added {
		t.Error("second Add reported Alex as newly added")
	}
	if names := s.Names(); len(names) != 1 {
		t.Errorf("Alex was stored twice: %v", names)
	}
}

// TestRemoveUnknown checks a distinguishable error, so the command can say "not
// an operator" instead of "could not save".
func TestRemoveUnknown(t *testing.T) {
	s, _ := store(t)
	if err := s.Remove("Nobody"); !errors.Is(err, ErrNotOperator) {
		t.Errorf("Remove unknown returned %v, want ErrNotOperator", err)
	}
}

// TestEmptyNameRejected guards against an entry nothing can ever match.
func TestEmptyNameRejected(t *testing.T) {
	s, _ := store(t)
	if _, err := s.Add("   ", ""); err == nil {
		t.Error("Add accepted a blank name")
	}
}

// TestFailedSaveRollsBack checks the in-memory list never claims something the
// file on disk does not agree with. Pointing the store at a path inside a
// read-only directory makes the write fail.
func TestFailedSaveRollsBack(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "ro")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := Load(filepath.Join(sub, "operators.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add("Steve", ""); err != nil {
		t.Fatalf("setup Add: %v", err)
	}
	if err := os.Chmod(sub, 0o500); err != nil {
		t.Skipf("cannot make the directory read-only: %v", err)
	}
	t.Cleanup(func() { os.Chmod(sub, 0o755) })

	if _, err := s.Add("Alex", ""); err == nil {
		t.Skip("the filesystem allowed the write anyway")
	}
	if s.IsOperator("Alex") {
		t.Error("Alex is an operator in memory though the save failed")
	}
	if !s.IsOperator("Steve") {
		t.Error("the rollback dropped Steve, who was already an operator")
	}
}
