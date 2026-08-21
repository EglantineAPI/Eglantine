// Package worlds provides a multi-world manager backed by a worlds/ directory.
//
// Dragonfly's Server owns exactly three worlds — an overworld, a nether and an
// end — and offers no way to add more. A Manager keeps those three registered
// under names alongside any number of extra worlds it opens itself, each in its
// own subdirectory with its own LevelDB provider.
package worlds

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb"

	"server/internal/gen"
)

// metaFile is the per-world file recording how a world was generated. Without
// it a world would come back with a different generator after a restart, and
// newly loaded chunks would not match the ones already on disk.
const metaFile = "eglantine.json"

// namePattern constrains world names to characters that are safe as a single
// path element, which keeps a name from escaping the worlds directory.
var namePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,32}$`)

var (
	// ErrExists is returned when creating a world whose name is taken.
	ErrExists = errors.New("a world with that name already exists")
	// ErrNotFound is returned when no world has the name passed.
	ErrNotFound = errors.New("no world with that name")
	// ErrBuiltIn is returned for operations that cannot apply to the three
	// worlds the Dragonfly server owns.
	ErrBuiltIn = errors.New("that world is built in and cannot be changed")
	// ErrInvalidName is returned for names that are unsafe as a path element.
	ErrInvalidName = errors.New("world names may only use letters, digits, _ and -, up to 32 characters")
)

// meta is the on-disk contents of metaFile.
type meta struct {
	Kind gen.Kind `json:"kind"`
	Seed int64    `json:"seed"`
}

// entry is one managed world.
type entry struct {
	name string
	w    *world.World
	meta meta
	// builtIn marks the three worlds owned by the Dragonfly server. The Manager
	// exposes them by name but must not close, delete or rename them.
	builtIn bool
}

// Manager owns the worlds directory and every world opened from it.
type Manager struct {
	dir string
	log *slog.Logger
	// workers is how many goroutines each world uses to load and generate
	// chunks. It has to match the server's, or a world created at runtime would
	// generate far more slowly than the ones opened at startup.
	workers int
	// entities is shared by every world. A world opened with a registry that
	// does not know a saved entity's type cannot load it back.
	entities world.EntityRegistry

	mu      sync.RWMutex
	entries map[string]*entry
	// def is the name resolved by Default, used as the fallback destination
	// when a player's world disappears from under them.
	def string
}

// New creates a Manager over dir, creating the directory if needed. The three
// built-in worlds passed are registered under the names given but stay owned by
// the server. Every other world already in dir is opened.
//
// The block registry must already be finalized, which server.Config.New does,
// so New must be called after the Server is constructed.
func New(dir string, log *slog.Logger, entities world.EntityRegistry, workers int, builtIn map[string]*world.World, def string) (*Manager, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create worlds directory: %w", err)
	}
	m := &Manager{dir: dir, log: log, entities: entities, workers: workers, entries: map[string]*entry{}, def: def}

	for name, w := range builtIn {
		m.entries[strings.ToLower(name)] = &entry{name: name, w: w, builtIn: true}
	}
	if err := m.loadExisting(); err != nil {
		return nil, err
	}
	return m, nil
}

// loadExisting opens every world directory that carries a metaFile. A directory
// without one belongs to something else — including the built-in worlds, whose
// data lives here but is opened by the server — and is skipped.
func (m *Manager) loadExisting() error {
	items, err := os.ReadDir(m.dir)
	if err != nil {
		return fmt.Errorf("read worlds directory: %w", err)
	}
	for _, item := range items {
		if !item.IsDir() {
			continue
		}
		name := item.Name()
		if _, ok := m.entries[strings.ToLower(name)]; ok {
			continue
		}
		md, err := m.readMeta(name)
		if err != nil {
			if !os.IsNotExist(err) {
				m.log.Warn("Skipping world with unreadable metadata.", "world", name, "error", err)
			}
			continue
		}
		if _, err := m.open(name, md); err != nil {
			// One broken world must not stop the server from starting.
			m.log.Error("Failed to open world.", "world", name, "error", err)
		}
	}
	return nil
}

func (m *Manager) path(name string) string { return filepath.Join(m.dir, name) }

func (m *Manager) readMeta(name string) (meta, error) {
	var md meta
	data, err := os.ReadFile(filepath.Join(m.path(name), metaFile))
	if err != nil {
		return md, err
	}
	if err := json.Unmarshal(data, &md); err != nil {
		return md, fmt.Errorf("decode %s: %w", metaFile, err)
	}
	if _, ok := gen.ParseKind(string(md.Kind)); !ok {
		return md, fmt.Errorf("unknown generator %q", md.Kind)
	}
	return md, nil
}

func (m *Manager) writeMeta(name string, md meta) error {
	data, err := json.MarshalIndent(md, "", "\t")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.path(name), metaFile), append(data, '\n'), 0o644)
}

// open opens the world directory named and registers it. The caller must hold
// no lock; open takes the write lock itself.
func (m *Manager) open(name string, md meta) (*world.World, error) {
	kind, ok := gen.ParseKind(string(md.Kind))
	if !ok {
		return nil, fmt.Errorf("unknown generator %q", md.Kind)
	}
	g, err := kind.New(md.Seed)
	if err != nil {
		return nil, err
	}
	log := m.log.With("world", name)
	provider, err := mcdb.Config{Log: log}.Open(m.path(name))
	if err != nil {
		return nil, fmt.Errorf("open world data: %w", err)
	}
	w := world.Config{
		Log:              log,
		Dim:              kind.Dimension(),
		Provider:         provider,
		Generator:        g,
		Entities:         m.entities,
		ChunkLoadWorkers: m.workers,
		SaveInterval:     time.Minute * 5,
	}.New()

	m.mu.Lock()
	m.entries[strings.ToLower(name)] = &entry{name: name, w: w, meta: md}
	m.mu.Unlock()
	return w, nil
}

// Create makes a new world and opens it.
func (m *Manager) Create(name string, kind gen.Kind, seed int64) (*world.World, error) {
	if !namePattern.MatchString(name) {
		return nil, ErrInvalidName
	}
	m.mu.RLock()
	_, taken := m.entries[strings.ToLower(name)]
	m.mu.RUnlock()
	if taken {
		return nil, ErrExists
	}
	if _, err := os.Stat(m.path(name)); err == nil {
		return nil, ErrExists
	}
	if err := os.MkdirAll(m.path(name), 0o755); err != nil {
		return nil, fmt.Errorf("create world directory: %w", err)
	}
	md := meta{Kind: kind, Seed: seed}
	if err := m.writeMeta(name, md); err != nil {
		// Leave nothing half-created behind.
		os.RemoveAll(m.path(name))
		return nil, fmt.Errorf("write world metadata: %w", err)
	}
	w, err := m.open(name, md)
	if err != nil {
		os.RemoveAll(m.path(name))
		return nil, err
	}
	m.log.Info("Created world.", "world", name, "generator", string(kind), "seed", seed)
	return w, nil
}

// World returns the world registered under the name passed, matched without
// regard to case.
func (m *Manager) World(name string) (*world.World, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.entries[strings.ToLower(name)]
	if !ok {
		return nil, false
	}
	return e.w, true
}

// Default returns the world players fall back to. It is the world named at
// construction, or any world at all if that name is gone.
func (m *Manager) Default() *world.World {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if e, ok := m.entries[strings.ToLower(m.def)]; ok {
		return e.w
	}
	for _, e := range m.entries {
		return e.w
	}
	return nil
}

// Names returns every registered world name, sorted.
func (m *Manager) Names() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.entries))
	for _, e := range m.entries {
		names = append(names, e.name)
	}
	sort.Strings(names)
	return names
}

// Info describes a world for display.
type Info struct {
	Name      string
	Kind      gen.Kind
	Seed      int64
	Dimension string
	BuiltIn   bool
}

// Info returns a description of the world named.
func (m *Manager) Info(name string) (Info, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.entries[strings.ToLower(name)]
	if !ok {
		return Info{}, false
	}
	kind := e.meta.Kind
	if e.builtIn {
		kind = "built-in"
	}
	return Info{
		Name:      e.name,
		Kind:      kind,
		Seed:      e.meta.Seed,
		Dimension: fmt.Sprint(e.w.Dimension()),
		BuiltIn:   e.builtIn,
	}, true
}

// Delete closes a world and removes it from disk. The caller is responsible for
// having moved every player out first; Delete does not check, because the
// Manager cannot see into a world without a transaction.
func (m *Manager) Delete(name string) error {
	m.mu.Lock()
	e, ok := m.entries[strings.ToLower(name)]
	if !ok {
		m.mu.Unlock()
		return ErrNotFound
	}
	if e.builtIn {
		m.mu.Unlock()
		return ErrBuiltIn
	}
	delete(m.entries, strings.ToLower(name))
	m.mu.Unlock()

	if err := e.w.Close(); err != nil {
		return fmt.Errorf("close world: %w", err)
	}
	if err := os.RemoveAll(m.path(e.name)); err != nil {
		return fmt.Errorf("remove world directory: %w", err)
	}
	m.log.Info("Deleted world.", "world", e.name)
	return nil
}

// Rename closes a world, moves its directory and opens it again under the new
// name. The world value changes, so callers holding the old *world.World must
// look it up again.
func (m *Manager) Rename(oldName, newName string) (*world.World, error) {
	if !namePattern.MatchString(newName) {
		return nil, ErrInvalidName
	}
	m.mu.Lock()
	e, ok := m.entries[strings.ToLower(oldName)]
	if !ok {
		m.mu.Unlock()
		return nil, ErrNotFound
	}
	if e.builtIn {
		m.mu.Unlock()
		return nil, ErrBuiltIn
	}
	if _, taken := m.entries[strings.ToLower(newName)]; taken {
		m.mu.Unlock()
		return nil, ErrExists
	}
	delete(m.entries, strings.ToLower(oldName))
	m.mu.Unlock()

	// The world has to be closed before the directory moves, so that LevelDB
	// releases its file handles and flushes.
	if err := e.w.Close(); err != nil {
		return nil, fmt.Errorf("close world: %w", err)
	}
	if err := os.Rename(m.path(e.name), m.path(newName)); err != nil {
		// Put the world back so a failed rename does not lose it.
		if w, reopenErr := m.open(e.name, e.meta); reopenErr != nil {
			m.log.Error("World lost after failed rename.", "world", e.name, "error", reopenErr)
		} else {
			_ = w
		}
		return nil, fmt.Errorf("move world directory: %w", err)
	}
	w, err := m.open(newName, e.meta)
	if err != nil {
		return nil, err
	}
	m.log.Info("Renamed world.", "from", e.name, "to", newName)
	return w, nil
}

// Close closes every world the Manager opened. The built-in worlds are left to
// the server, which closes them itself.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var errs []error
	for key, e := range m.entries {
		if e.builtIn {
			continue
		}
		if err := e.w.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close %s: %w", e.name, err))
		}
		delete(m.entries, key)
	}
	return errors.Join(errs...)
}

// seedFile holds the seed of the three worlds the Dragonfly server owns.
const seedFile = ".seed"

// LoadSeed returns the seed for the server's built-in worlds, creating and
// storing a random one the first time.
//
// The seed has to persist. Chunks already generated stay on disk, so a seed
// that changed between restarts would leave every newly generated chunk
// mismatched against its neighbours along a visible seam.
func LoadSeed(dir string) (int64, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, fmt.Errorf("create worlds directory: %w", err)
	}
	path := filepath.Join(dir, seedFile)

	data, err := os.ReadFile(path)
	if err == nil {
		var seed int64
		if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &seed); err == nil {
			return seed, nil
		}
		// A corrupt seed file is replaced rather than treated as fatal, but the
		// world it described is effectively gone, so this is worth reporting.
		return 0, fmt.Errorf("seed file %s is corrupt; remove it to start over", path)
	}
	if !os.IsNotExist(err) {
		return 0, fmt.Errorf("read seed: %w", err)
	}

	seed := rand.Int64()
	if err := os.WriteFile(path, []byte(fmt.Sprintf("%d\n", seed)), 0o644); err != nil {
		return 0, fmt.Errorf("write seed: %w", err)
	}
	return seed, nil
}

// InfoForWorld returns the description of a world by identity rather than by
// name. Commands run inside a transaction know the *world.World they are in but
// not what the Manager calls it.
func (m *Manager) InfoForWorld(w *world.World) (Info, bool) {
	m.mu.RLock()
	var name string
	for _, e := range m.entries {
		if e.w == w {
			name = e.name
			break
		}
	}
	m.mu.RUnlock()
	if name == "" {
		return Info{}, false
	}
	return m.Info(name)
}
