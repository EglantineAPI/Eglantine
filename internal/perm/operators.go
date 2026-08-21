// Package perm stores the server's operator list.
package perm

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ErrNotOperator is returned when removing someone who is not an operator.
var ErrNotOperator = errors.New("that player is not an operator")

// Operator is one entry of the operator list.
type Operator struct {
	Name string `json:"name"`
	// XUID is recorded when known. Lookups go by name, because that is what an
	// admin types and what the console has to work with for offline players,
	// but the XUID makes an entry traceable to an account after a rename.
	XUID string `json:"xuid,omitempty"`
}

// Store is the operator list, backed by a JSON file. Every mutation is written
// through to disk, so the list survives a crash rather than only a clean stop.
type Store struct {
	path string

	mu  sync.RWMutex
	ops map[string]Operator
}

// Load reads the operator file at the path passed. A missing file yields an
// empty Store rather than an error, so a fresh server starts with no operators.
func Load(path string) (*Store, error) {
	s := &Store{path: path, ops: map[string]Operator{}}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	// An empty file is treated as an empty list; json.Unmarshal would reject it.
	if len(strings.TrimSpace(string(data))) == 0 {
		return s, nil
	}
	var list []Operator
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	for _, op := range list {
		if op.Name == "" {
			continue
		}
		s.ops[strings.ToLower(op.Name)] = op
	}
	return s, nil
}

// save writes the list to disk. The caller must hold at least a read lock.
//
// The write goes to a temporary file that is then renamed over the target, so
// an interrupted write cannot leave a truncated operator list behind.
func (s *Store) save() error {
	list := make([]Operator, 0, len(s.ops))
	for _, op := range s.ops {
		list = append(list, op)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })

	data, err := json.MarshalIndent(list, "", "\t")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".operators-*.json")
	if err != nil {
		return fmt.Errorf("create temporary operator file: %w", err)
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return fmt.Errorf("write operator file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return fmt.Errorf("close operator file: %w", err)
	}
	if err := os.Rename(name, s.path); err != nil {
		os.Remove(name)
		return fmt.Errorf("replace operator file: %w", err)
	}
	return nil
}

// IsOperator reports whether the player name passed is an operator.
func (s *Store) IsOperator(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.ops[strings.ToLower(name)]
	return ok
}

// Add makes a player an operator and writes the list to disk. It reports
// whether the player was newly added; adding an existing operator is not an
// error, it simply refreshes the recorded XUID.
func (s *Store) Add(name, xuid string) (bool, error) {
	if strings.TrimSpace(name) == "" {
		return false, errors.New("empty player name")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	key := strings.ToLower(name)
	_, existed := s.ops[key]
	s.ops[key] = Operator{Name: name, XUID: xuid}
	if err := s.save(); err != nil {
		// Roll the in-memory list back, so it never claims something the file
		// on disk does not agree with.
		if existed {
			s.ops[key] = Operator{Name: name}
		} else {
			delete(s.ops, key)
		}
		return false, err
	}
	return !existed, nil
}

// Remove revokes operator status and writes the list to disk.
func (s *Store) Remove(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := strings.ToLower(name)
	old, ok := s.ops[key]
	if !ok {
		return ErrNotOperator
	}
	delete(s.ops, key)
	if err := s.save(); err != nil {
		s.ops[key] = old
		return err
	}
	return nil
}

// Names returns every operator name, sorted.
func (s *Store) Names() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.ops))
	for _, op := range s.ops {
		names = append(names, op.Name)
	}
	sort.Strings(names)
	return names
}
