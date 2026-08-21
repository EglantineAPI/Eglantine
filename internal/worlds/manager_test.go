package worlds

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/df-mc/dragonfly/server/world"

	"server/internal/gen"
)

// TestMain finalizes the block registry, which generators need to resolve
// blocks to runtime IDs. The running server does this in server.Config.New.
func TestMain(m *testing.M) {
	world.DefaultBlockRegistry.Finalize()
	os.Exit(m.Run())
}

func manager(t *testing.T) (*Manager, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "worlds")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	m, err := New(dir, log, nil, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { m.Close() })
	return m, dir
}

// TestCreateAndLookup checks a created world is registered and on disk.
func TestCreateAndLookup(t *testing.T) {
	m, dir := manager(t)
	if _, err := m.Create("ilha", gen.KindFlat, 42); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, ok := m.World("ilha"); !ok {
		t.Error("created world is not registered")
	}
	// Names are matched without regard to case, as players type them.
	if _, ok := m.World("ILHA"); !ok {
		t.Error("lookup is case sensitive")
	}
	if _, err := os.Stat(filepath.Join(dir, "ilha", metaFile)); err != nil {
		t.Errorf("metadata was not written: %v", err)
	}

	info, ok := m.Info("ilha")
	if !ok || info.Kind != gen.KindFlat || info.Seed != 42 {
		t.Errorf("Info = %+v", info)
	}
}

// TestRejectsUnsafeNames is the guard that keeps a world name from escaping the
// worlds directory, since the name becomes a path element.
func TestRejectsUnsafeNames(t *testing.T) {
	m, _ := manager(t)
	for _, name := range []string{"..", "../escape", "a/b", "", "with space", "dot.dot", string(make([]byte, 33))} {
		if _, err := m.Create(name, gen.KindVoid, 1); !errors.Is(err, ErrInvalidName) {
			t.Errorf("Create(%q) = %v, want ErrInvalidName", name, err)
		}
	}
}

// TestDuplicateRejected checks a name cannot be taken twice.
func TestDuplicateRejected(t *testing.T) {
	m, _ := manager(t)
	if _, err := m.Create("a", gen.KindVoid, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create("a", gen.KindVoid, 2); !errors.Is(err, ErrExists) {
		t.Errorf("second Create = %v, want ErrExists", err)
	}
}

// TestReopenRestoresWorlds checks worlds come back after a restart with the
// generator and seed they were created with.
func TestReopenRestoresWorlds(t *testing.T) {
	m, dir := manager(t)
	if _, err := m.Create("terreno", gen.KindOverworld, 99); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	again, err := New(dir, log, nil, "")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer again.Close()

	info, ok := again.Info("terreno")
	if !ok {
		t.Fatal("world did not come back after a restart")
	}
	if info.Kind != gen.KindOverworld || info.Seed != 99 {
		t.Errorf("world came back as %+v, want overworld seed 99", info)
	}
}

// TestRenameMovesDirectory checks the rename moves the data rather than only
// relabelling the entry.
func TestRenameMovesDirectory(t *testing.T) {
	m, dir := manager(t)
	if _, err := m.Create("antigo", gen.KindFlat, 5); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Rename("antigo", "novo"); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	if _, ok := m.World("antigo"); ok {
		t.Error("the old name is still registered")
	}
	if _, ok := m.World("novo"); !ok {
		t.Error("the new name is not registered")
	}
	if _, err := os.Stat(filepath.Join(dir, "antigo")); !os.IsNotExist(err) {
		t.Error("the old directory is still on disk")
	}
	// The seed has to survive, or the renamed world would generate new chunks
	// that do not match the ones already saved.
	if info, _ := m.Info("novo"); info.Seed != 5 {
		t.Errorf("seed after rename is %d, want 5", info.Seed)
	}
}

// TestRenameOntoTakenName checks a rename cannot clobber another world.
func TestRenameOntoTakenName(t *testing.T) {
	m, _ := manager(t)
	if _, err := m.Create("a", gen.KindVoid, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create("b", gen.KindVoid, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Rename("a", "b"); !errors.Is(err, ErrExists) {
		t.Errorf("Rename onto a taken name = %v, want ErrExists", err)
	}
	// The world being renamed must survive a rejected rename.
	if _, ok := m.World("a"); !ok {
		t.Error("the source world was lost by a rejected rename")
	}
}

// TestDeleteRemovesDirectory checks delete clears both the registry and disk.
func TestDeleteRemovesDirectory(t *testing.T) {
	m, dir := manager(t)
	if _, err := m.Create("temp", gen.KindVoid, 1); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete("temp"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := m.World("temp"); ok {
		t.Error("the world is still registered")
	}
	if _, err := os.Stat(filepath.Join(dir, "temp")); !os.IsNotExist(err) {
		t.Error("the directory is still on disk")
	}
}

// TestUnknownWorldErrors checks the not-found path.
func TestUnknownWorldErrors(t *testing.T) {
	m, _ := manager(t)
	if err := m.Delete("ghost"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete unknown = %v, want ErrNotFound", err)
	}
	if _, err := m.Rename("ghost", "other"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Rename unknown = %v, want ErrNotFound", err)
	}
}

// TestSeedIsStable checks the built-in worlds keep one seed across restarts.
// A changing seed would make newly generated chunks mismatch the saved ones.
func TestSeedIsStable(t *testing.T) {
	dir := t.TempDir()
	first, err := LoadSeed(dir)
	if err != nil {
		t.Fatalf("LoadSeed: %v", err)
	}
	second, err := LoadSeed(dir)
	if err != nil {
		t.Fatalf("second LoadSeed: %v", err)
	}
	if first != second {
		t.Errorf("seed changed between calls: %d then %d", first, second)
	}
}

// TestStrayDirectoryIgnored checks a directory without metadata is skipped
// rather than opened as a world or treated as a fatal error.
func TestStrayDirectoryIgnored(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "notaworld"), 0o755); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	m, err := New(dir, log, nil, "")
	if err != nil {
		t.Fatalf("New with a stray directory: %v", err)
	}
	defer m.Close()
	if _, ok := m.World("notaworld"); ok {
		t.Error("a directory without metadata was opened as a world")
	}
}
