package main

import (
	"fmt"
	"log/slog"
	"os"
	"runtime"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/pelletier/go-toml/v2"

	"server/internal/commands"
	"server/internal/console"
	"server/internal/gen"
	"server/internal/mob"
	"server/internal/perm"
	"server/internal/worlds"
)

// worldsDir holds every world. The three worlds the Dragonfly server owns live
// in the subdirectory named by config.toml; worlds created with /world create
// get a subdirectory each.
const worldsDir = "worlds"

// operatorsFile is the operator list, in the working directory beside
// config.toml.
const operatorsFile = "operators.json"

func main() {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	chat.Global.Subscribe(chat.StdoutSubscriber{})
	log := slog.Default()

	conf, err := readConfig(log)
	if err != nil {
		panic(err)
	}

	// The built-in worlds share one seed, kept on disk so that terrain stays
	// continuous across restarts.
	seed, err := worlds.LoadSeed(worldsDir)
	if err != nil {
		panic(err)
	}
	conf.Generator = func(dim world.Dimension) world.Generator {
		return generatorFor(dim, seed)
	}
	// Mobs have to be registered before any world opens, or a world that has
	// one saved in it cannot read it back.
	entities := mob.Registry()
	conf.Entities = entities
	conf.ChunkLoadWorkers = chunkWorkers()

	// conf.New finalizes the block registry. Generators resolve blocks to
	// runtime IDs, which panics before that, so nothing may build one until
	// this call has returned.
	srv := conf.New()

	mgr, err := worlds.New(worldsDir, log, entities, conf.ChunkLoadWorkers, map[string]*world.World{
		"world":  srv.World(),
		"nether": srv.Nether(),
		"end":    srv.End(),
	}, "world")
	if err != nil {
		panic(err)
	}

	ops, err := perm.Load(operatorsFile)
	if err != nil {
		panic(err)
	}
	// The console's stop callback and the command's /stop share one path.
	shutdown := func() {
		if err := srv.Close(); err != nil {
			log.Error("Could not close the server.", "error", err)
		}
	}
	commands.Register(mgr, ops, log, srv, seed, shutdown)

	srv.CloseOnProgramEnd()

	// The console shuts the server down on "stop" and on end of input. Closing
	// the server ends the Accept loop below, and the deferred close of the
	// manager then runs.
	term := console.New(log, shutdown)
	go term.Run()

	srv.Listen()
	for p := range srv.Accept() {
		// Operators can be added by name while offline, so the XUID is only
		// available now. NoteXUID fills it in for those entries and leaves
		// everyone else alone.
		if err := ops.NoteXUID(p.Name(), p.XUID()); err != nil {
			log.Error("Could not record operator XUID.", "player", p.Name(), "error", err)
		}
	}

	if err := mgr.Close(); err != nil {
		log.Error("Could not close worlds.", "error", err)
	}
}

// chunkWorkers returns how many goroutines should generate and load chunks.
//
// Dragonfly defaults to one, and a single worker is also the case where it
// serialises the generator behind a mutex. A joining player asks for the whole
// square of chunks around them at once — over a thousand even at a modest view
// distance — so one worker means the player waits for all of them in sequence
// and the world appears frozen until it finishes.
//
// One core is left to the world tick, so generating chunks cannot starve the
// server itself.
func chunkWorkers() int {
	if n := runtime.NumCPU() - 1; n > 1 {
		return n
	}
	return 2
}

// generatorFor returns the generator for one of the server's built-in worlds.
func generatorFor(dim world.Dimension, seed int64) world.Generator {
	var kind gen.Kind
	switch dim {
	case world.Nether:
		kind = gen.KindNether
	case world.End:
		kind = gen.KindEnd
	default:
		kind = gen.KindOverworld
	}
	g, err := kind.New(seed)
	if err != nil {
		// The kinds above are constants, so this cannot fire unless the
		// registry and this switch have drifted apart.
		panic(fmt.Sprintf("build %s generator: %v", kind, err))
	}
	return g
}

// readConfig reads the configuration from the config.toml file, or creates the
// file if it does not yet exist.
func readConfig(log *slog.Logger) (server.Config, error) {
	c := server.DefaultConfig()
	c.World.Folder = worldsDir + "/world"
	var zero server.Config
	if _, err := os.Stat("config.toml"); os.IsNotExist(err) {
		data, err := toml.Marshal(c)
		if err != nil {
			return zero, fmt.Errorf("encode default config: %v", err)
		}
		if err := os.WriteFile("config.toml", data, 0644); err != nil {
			return zero, fmt.Errorf("create default config: %v", err)
		}
		return c.Config(log)
	}
	data, err := os.ReadFile("config.toml")
	if err != nil {
		return zero, fmt.Errorf("read config: %v", err)
	}
	if err := toml.Unmarshal(data, &c); err != nil {
		return zero, fmt.Errorf("decode config: %v", err)
	}
	return c.Config(log)
}
