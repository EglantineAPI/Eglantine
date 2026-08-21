// Package commands registers Eglantine's in-game and console commands.
package commands

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"

	"server/internal/gen"
	"server/internal/perm"
	"server/internal/worlds"
)

// The command system builds Runnable values by reflection, so a Runnable
// cannot carry references of its own. The dependencies commands need are held
// here instead, set once by Register before any command can run.
var (
	manager *worlds.Manager
	ops     *perm.Store
	log     *slog.Logger
)

// Register wires the commands to their dependencies and registers them. It must
// be called once, before the server starts accepting players.
func Register(m *worlds.Manager, o *perm.Store, l *slog.Logger) {
	manager, ops, log = m, o, l

	cmd.Register(cmd.New("world", "Manage the worlds on this server.", []string{"w"},
		worldList{}, worldInfo{}, worldCreate{}, worldTeleport{}, worldDelete{}, worldRename{},
	))
	cmd.Register(cmd.New("op", "Grant a player operator status.", nil, opCommand{}))
	cmd.Register(cmd.New("deop", "Revoke a player's operator status.", nil, deopCommand{}))
}

// operator reports whether a command source may run operator commands. The
// console is always allowed: it is only reachable by whoever runs the process.
func operator(src cmd.Source) bool {
	p, ok := src.(*player.Player)
	if !ok {
		return true
	}
	return ops.IsOperator(p.Name())
}

// worldName is an enum of the world names currently registered. Implementing
// cmd.Enum makes the client complete the names as they are typed.
type worldName string

func (worldName) Type() string { return "world" }

func (worldName) Options(cmd.Source) []string { return manager.Names() }

// generatorKind is an enum of the available generators.
type generatorKind string

func (generatorKind) Type() string { return "generator" }

func (generatorKind) Options(cmd.Source) []string { return gen.Kinds() }

// resolve looks up the world for an enum value, writing the error to o itself
// when there is none.
func resolve(name worldName, o *cmd.Output) (*world.World, bool) {
	w, ok := manager.World(string(name))
	if !ok {
		o.Errorf("There is no world named %q.", string(name))
		return nil, false
	}
	return w, true
}

// worldList lists every world.
type worldList struct {
	Sub cmd.SubCommand `cmd:"list"`
}

func (worldList) Allow(src cmd.Source) bool { return operator(src) }

func (worldList) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	names := manager.Names()
	if len(names) == 0 {
		o.Print("There are no worlds.")
		return
	}
	o.Printf("Worlds (%d):", len(names))
	for _, name := range names {
		info, ok := manager.Info(name)
		if !ok {
			continue
		}
		o.Printf("  %s - %s (%s)", info.Name, info.Kind, info.Dimension)
	}
}

// worldInfo describes a single world.
type worldInfo struct {
	Sub   cmd.SubCommand `cmd:"info"`
	World worldName      `cmd:"world"`
}

func (worldInfo) Allow(src cmd.Source) bool { return operator(src) }

func (c worldInfo) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	info, ok := manager.Info(string(c.World))
	if !ok {
		o.Errorf("There is no world named %q.", string(c.World))
		return
	}
	o.Printf("World %s", info.Name)
	o.Printf("  generator: %s", info.Kind)
	o.Printf("  dimension: %s", info.Dimension)
	if info.BuiltIn {
		o.Print("  built in: yes (cannot be renamed or deleted)")
	} else {
		o.Printf("  seed: %d", info.Seed)
	}
}

// worldCreate creates and opens a new world.
type worldCreate struct {
	Sub       cmd.SubCommand    `cmd:"create"`
	NewName   string            `cmd:"name"`
	Generator generatorKind     `cmd:"generator"`
	Seed      cmd.Optional[int] `cmd:"seed"`
}

func (worldCreate) Allow(src cmd.Source) bool { return operator(src) }

func (c worldCreate) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	kind, ok := gen.ParseKind(string(c.Generator))
	if !ok {
		o.Errorf("Unknown generator %q. Available: %v", string(c.Generator), gen.Kinds())
		return
	}
	// A seed of zero from the player is honoured; only an omitted seed is
	// randomised, so a world can be recreated deliberately.
	seed := int64(c.Seed.LoadOr(int(rand.Int64())))

	if _, err := manager.Create(c.NewName, kind, seed); err != nil {
		o.Errorf("Could not create world: %v", err)
		return
	}
	o.Printf("Created world %s using the %s generator, seed %d.", c.NewName, kind, seed)
}

// worldTeleport moves a player into another world.
type worldTeleport struct {
	Sub     cmd.SubCommand             `cmd:"tp"`
	World   worldName                  `cmd:"world"`
	Targets cmd.Optional[[]cmd.Target] `cmd:"player"`
}

func (worldTeleport) Allow(src cmd.Source) bool { return operator(src) }

func (c worldTeleport) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	w, ok := resolve(c.World, o)
	if !ok {
		return
	}
	if tx == nil {
		o.Error("The console is not in a world; name a player to teleport.")
		return
	}

	if targets, given := c.Targets.Load(); given {
		if len(targets) == 0 {
			o.Error("No player matched.")
			return
		}
		for _, t := range targets {
			p, ok := t.(*player.Player)
			if !ok {
				continue
			}
			transfer(p, tx, w, o)
		}
		return
	}

	p, ok := src.(*player.Player)
	if !ok {
		o.Error("Name a player to teleport when running this from the console.")
		return
	}
	transfer(p, tx, w, o)
}

// transfer moves a player out of the transaction's world and into w, landing
// them on that world's spawn.
//
// The player is removed from the source world before the destination accepts
// them, so a destination that refuses must be handled or the player is lost.
func transfer(p *player.Player, tx *world.Tx, w *world.World, o *cmd.Output) {
	if tx.World() == w {
		o.Printf("%s is already in that world.", p.Name())
		return
	}
	// Read everything needed off the player first: once RemoveEntity returns,
	// p belongs to no transaction and must not be touched.
	name, pos := p.Name(), w.Spawn().Vec3Middle()
	handle := tx.RemoveEntity(p)

	task := w.Do(func(tx *world.Tx) {
		tx.AddEntityAt(handle, pos)
	})
	if errors.Is(task.Err(), world.ErrWorldClosed) {
		// The destination refused straight away. Put the player back through
		// the transaction that is still open rather than dropping them.
		tx.AddEntity(handle)
		o.Errorf("World %s is closed.", w.Name())
		return
	}
	o.Printf("Moved %s to %s.", name, w.Name())
}

// evacuate moves every player out of w and into the manager's default world.
// It returns the task doing the work, which completes once the world is empty.
//
// A world must be emptied before it is closed, since deleting or renaming a
// world with players still inside would strand them.
func evacuate(w *world.World) *world.Task {
	fallback := manager.Default()
	return w.Do(func(tx *world.Tx) {
		if fallback == nil || fallback == w {
			return
		}
		pos := fallback.Spawn().Vec3Middle()
		// Collect first: removing entities while ranging over the world's
		// players would mutate the sequence being iterated.
		var players []*player.Player
		for e := range tx.Players() {
			if p, ok := e.(*player.Player); ok {
				players = append(players, p)
			}
		}
		for _, p := range players {
			handle := tx.RemoveEntity(p)
			fallback.Do(func(tx *world.Tx) {
				tx.AddEntityAt(handle, pos)
			})
		}
	})
}

// evacuateTimeout bounds the wait for a world to empty, so a wedged world
// cannot hang the console forever.
const evacuateTimeout = 15 * time.Second

// withEmptyWorld empties w and then runs op on it.
//
// From the console there is no transaction to block, so the work is done inline
// and its real result is reported. From in-game the command is running inside
// its own world's transaction, which must not be blocked waiting on another
// world to tick, so the work is deferred and its outcome goes to the log.
func withEmptyWorld(w *world.World, tx *world.Tx, o *cmd.Output, pending string, op func() error) {
	task := evacuate(w)
	if tx != nil {
		o.Print(pending)
		task.OnDone(func(err error) {
			if err != nil {
				log.Error("Could not move players out of world.", "world", w.Name(), "error", err)
				return
			}
			if err := op(); err != nil {
				log.Error("World operation failed.", "world", w.Name(), "error", err)
			}
		})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), evacuateTimeout)
	defer cancel()
	if err := task.Wait(ctx); err != nil {
		o.Errorf("Could not move players out of the world: %v", err)
		return
	}
	if err := op(); err != nil {
		o.Errorf("%v", err)
	}
}

// worldDelete removes a world from disk.
type worldDelete struct {
	Sub   cmd.SubCommand `cmd:"delete"`
	World worldName      `cmd:"world"`
}

func (worldDelete) Allow(src cmd.Source) bool { return operator(src) }

func (c worldDelete) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	w, ok := resolve(c.World, o)
	if !ok {
		return
	}
	if tx != nil && tx.World() == w {
		o.Error("Leave the world before deleting it.")
		return
	}
	name := string(c.World)
	if info, ok := manager.Info(name); ok && info.BuiltIn {
		o.Errorf("%s is built in and cannot be deleted.", info.Name)
		return
	}

	withEmptyWorld(w, tx, o,
		fmt.Sprintf("Deleting %s. Any players inside are being moved out first.", name),
		func() error {
			if err := manager.Delete(name); err != nil {
				return fmt.Errorf("could not delete %s: %w", name, err)
			}
			o.Printf("Deleted world %s.", name)
			return nil
		})
}

// worldRename renames a world on disk.
type worldRename struct {
	Sub     cmd.SubCommand `cmd:"rename"`
	World   worldName      `cmd:"world"`
	NewName string         `cmd:"newname"`
}

func (worldRename) Allow(src cmd.Source) bool { return operator(src) }

func (c worldRename) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	w, ok := resolve(c.World, o)
	if !ok {
		return
	}
	if tx != nil && tx.World() == w {
		o.Error("Leave the world before renaming it.")
		return
	}
	from, to := string(c.World), c.NewName
	if info, ok := manager.Info(from); ok && info.BuiltIn {
		o.Errorf("%s is built in and cannot be renamed.", info.Name)
		return
	}

	withEmptyWorld(w, tx, o,
		fmt.Sprintf("Renaming %s to %s. Any players inside are being moved out first.", from, to),
		func() error {
			if _, err := manager.Rename(from, to); err != nil {
				return fmt.Errorf("could not rename %s: %w", from, err)
			}
			o.Printf("Renamed %s to %s.", from, to)
			return nil
		})
}

// opCommand grants operator status.
type opCommand struct {
	Target []cmd.Target `cmd:"player"`
}

func (opCommand) Allow(src cmd.Source) bool { return operator(src) }

func (c opCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	if len(c.Target) == 0 {
		o.Error("No player matched.")
		return
	}
	for _, t := range c.Target {
		p, ok := t.(*player.Player)
		if !ok {
			continue
		}
		added, err := ops.Add(p.Name(), p.XUID())
		if err != nil {
			o.Errorf("Could not save the operator list: %v", err)
			return
		}
		if !added {
			o.Printf("%s is already an operator.", p.Name())
			continue
		}
		o.Printf("%s is now an operator.", p.Name())
		p.Message("You are now an operator.")
	}
}

// deopCommand revokes operator status. It takes a plain name rather than a
// target selector, so an operator who is currently offline can still be
// removed.
type deopCommand struct {
	PlayerName string `cmd:"player"`
}

func (deopCommand) Allow(src cmd.Source) bool { return operator(src) }

func (c deopCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	if err := ops.Remove(c.PlayerName); err != nil {
		if errors.Is(err, perm.ErrNotOperator) {
			o.Errorf("%s is not an operator.", c.PlayerName)
			return
		}
		o.Errorf("Could not save the operator list: %v", err)
		return
	}
	o.Printf("%s is no longer an operator.", c.PlayerName)
}

// Both enums must satisfy cmd.Enum for the client to complete their values.
var (
	_ cmd.Enum = worldName("")
	_ cmd.Enum = generatorKind("")
)
