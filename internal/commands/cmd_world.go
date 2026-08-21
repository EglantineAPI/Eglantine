package commands

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// maxFillVolume bounds /fill and /clone. A command that can rewrite millions of
// blocks inside one transaction would stall the world tick for as long as it
// takes, so the limit is refused up front rather than discovered by the server
// freezing. Vanilla caps these at 32768 for the same reason.
const maxFillVolume = 32768

// consoleWorldTimeout bounds how long a console command waits for the world to
// run its work, so a wedged world cannot hang the terminal.
const consoleWorldTimeout = 10 * time.Second

// inWorld runs f against a world transaction.
//
// A player's command already has one. The console has none, so its commands
// would otherwise be unable to touch the world at all; they are run against the
// default world instead, which is what an operator administering the server
// from the terminal expects. Waiting is safe there precisely because there is
// no transaction of our own to block.
func inWorld(tx *world.Tx, o *cmd.Output, f func(tx *world.Tx)) {
	if tx != nil {
		f(tx)
		return
	}
	w := manager.Default()
	if w == nil {
		o.Error("There is no world to run this in.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), consoleWorldTimeout)
	defer cancel()
	if err := w.Do(f).Wait(ctx); err != nil {
		o.Errorf("The world did not run the command: %v", err)
	}
}

// timeSet is /time set.
type timeSet struct {
	Sub  cmd.SubCommand `cmd:"set"`
	Time string         `cmd:"time"`
}

func (timeSet) Allow(src cmd.Source) bool { return operator(src) }

func (c timeSet) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	inWorld(tx, o, func(tx *world.Tx) {
		value, ok := namedTimes[strings.ToLower(c.Time)]
		if !ok {
			n, err := strconv.Atoi(c.Time)
			if err != nil {
				o.Errorf("%q is neither a tick count nor a time of day.", c.Time)
				return
			}
			value = n
		}
		tx.World().SetTime(value)
		o.Printf("Set the time to %d.", value)
	})
}

// timeAdd is /time add.
type timeAdd struct {
	Sub    cmd.SubCommand `cmd:"add"`
	Amount int            `cmd:"amount"`
}

func (timeAdd) Allow(src cmd.Source) bool { return operator(src) }

func (c timeAdd) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	inWorld(tx, o, func(tx *world.Tx) {
		w := tx.World()
		w.SetTime(w.Time() + c.Amount)
		o.Printf("Added %d to the time, now %d.", c.Amount, w.Time())
	})
}

// timeQuery is /time query.
type timeQuery struct {
	Sub   cmd.SubCommand `cmd:"query"`
	Which cmd.Optional[timeSpecEnum]
}

func (timeQuery) Allow(src cmd.Source) bool { return operator(src) }

func (timeQuery) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	inWorld(tx, o, func(tx *world.Tx) {
		w := tx.World()
		o.Printf("The time is %d (day %d).", w.Time(), w.Time()/24000)
	})
}

// weatherCommand is /weather.
type weatherCommand struct {
	State    weatherEnum       `cmd:"state"`
	Duration cmd.Optional[int] `cmd:"duration"`
}

func (weatherCommand) Allow(src cmd.Source) bool { return operator(src) }

func (c weatherCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	inWorld(tx, o, func(tx *world.Tx) {
		w := tx.World()
		// Vanilla's duration is in seconds and defaults to a little over five
		// minutes of game weather.
		d := time.Duration(c.Duration.LoadOr(360)) * time.Second

		switch strings.ToLower(string(c.State)) {
		case "clear":
			w.StopThundering()
			w.StopRaining()
			o.Print("The weather is now clear.")
		case "rain":
			w.StopThundering()
			w.StartRaining(d)
			o.Printf("It is now raining for %s.", d)
		case "thunder":
			w.StartRaining(d)
			w.StartThundering(d)
			o.Printf("It is now thundering for %s.", d)
		default:
			o.Errorf("Unknown weather %q.", string(c.State))
		}
	})
}

// difficultyCommand is /difficulty.
type difficultyCommand struct {
	Level difficultyEnum `cmd:"difficulty"`
}

func (difficultyCommand) Allow(src cmd.Source) bool { return operator(src) }

func (c difficultyCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	inWorld(tx, o, func(tx *world.Tx) {
		d, ok := difficulties[strings.ToLower(string(c.Level))]
		if !ok {
			o.Errorf("Unknown difficulty %q.", string(c.Level))
			return
		}
		tx.World().SetDifficulty(d)
		o.Printf("Set the difficulty to %s.", string(c.Level))
	})
}

// setBlockCommand is /setblock.
type setBlockCommand struct {
	Position mgl64.Vec3 `cmd:"position"`
	Block    blockEnum  `cmd:"block"`
}

func (setBlockCommand) Allow(src cmd.Source) bool { return operator(src) }

func (c setBlockCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	inWorld(tx, o, func(tx *world.Tx) {
		b, ok := lookupBlock(string(c.Block))
		if !ok {
			o.Errorf("Unknown block %q.", string(c.Block))
			return
		}
		pos := cube.PosFromVec3(c.Position)
		if !inRange(tx, pos) {
			o.Errorf("%v is outside the world.", pos)
			return
		}
		tx.SetBlock(pos, b, nil)
		o.Printf("Set %v to %s.", pos, string(c.Block))
	})
}

// fillCommand is /fill.
type fillCommand struct {
	From  mgl64.Vec3 `cmd:"from"`
	To    mgl64.Vec3 `cmd:"to"`
	Block blockEnum  `cmd:"block"`
}

func (fillCommand) Allow(src cmd.Source) bool { return operator(src) }

func (c fillCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	inWorld(tx, o, func(tx *world.Tx) {
		b, ok := lookupBlock(string(c.Block))
		if !ok {
			o.Errorf("Unknown block %q.", string(c.Block))
			return
		}
		lo, hi := bounds(c.From, c.To)
		volume := (hi[0] - lo[0] + 1) * (hi[1] - lo[1] + 1) * (hi[2] - lo[2] + 1)
		if volume > maxFillVolume {
			o.Errorf("That is %d blocks; the limit is %d.", volume, maxFillVolume)
			return
		}

		filled := 0
		for x := lo[0]; x <= hi[0]; x++ {
			for y := lo[1]; y <= hi[1]; y++ {
				for z := lo[2]; z <= hi[2]; z++ {
					pos := cube.Pos{x, y, z}
					if !inRange(tx, pos) {
						continue
					}
					tx.SetBlock(pos, b, nil)
					filled++
				}
			}
		}
		o.Printf("Filled %d blocks with %s.", filled, string(c.Block))
	})
}

// cloneCommand is /clone.
type cloneCommand struct {
	Begin       mgl64.Vec3 `cmd:"begin"`
	End         mgl64.Vec3 `cmd:"end"`
	Destination mgl64.Vec3 `cmd:"destination"`
}

func (cloneCommand) Allow(src cmd.Source) bool { return operator(src) }

func (c cloneCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	inWorld(tx, o, func(tx *world.Tx) {
		lo, hi := bounds(c.Begin, c.End)
		volume := (hi[0] - lo[0] + 1) * (hi[1] - lo[1] + 1) * (hi[2] - lo[2] + 1)
		if volume > maxFillVolume {
			o.Errorf("That is %d blocks; the limit is %d.", volume, maxFillVolume)
			return
		}
		dst := cube.PosFromVec3(c.Destination)

		// The source is read in full before anything is written, so a destination
		// overlapping the source copies the original rather than blocks this same
		// command has already changed.
		type placed struct {
			pos cube.Pos
			b   world.Block
		}
		buf := make([]placed, 0, volume)
		for x := lo[0]; x <= hi[0]; x++ {
			for y := lo[1]; y <= hi[1]; y++ {
				for z := lo[2]; z <= hi[2]; z++ {
					src := cube.Pos{x, y, z}
					if !inRange(tx, src) {
						continue
					}
					target := cube.Pos{dst[0] + x - lo[0], dst[1] + y - lo[1], dst[2] + z - lo[2]}
					if !inRange(tx, target) {
						continue
					}
					buf = append(buf, placed{pos: target, b: tx.Block(src)})
				}
			}
		}
		for _, p := range buf {
			tx.SetBlock(p.pos, p.b, nil)
		}
		o.Printf("Cloned %d blocks.", len(buf))
	})
}

// bounds normalises two corners into a low and a high corner.
func bounds(a, b mgl64.Vec3) (cube.Pos, cube.Pos) {
	p, q := cube.PosFromVec3(a), cube.PosFromVec3(b)
	lo := cube.Pos{minIntC(p[0], q[0]), minIntC(p[1], q[1]), minIntC(p[2], q[2])}
	hi := cube.Pos{maxIntC(p[0], q[0]), maxIntC(p[1], q[1]), maxIntC(p[2], q[2])}
	return lo, hi
}

// inRange reports whether a position is inside the world's build range.
func inRange(tx *world.Tx, pos cube.Pos) bool {
	r := tx.Range()
	return pos[1] >= r.Min() && pos[1] <= r.Max()
}

// seedCommand is /seed.
type seedCommand struct{}

func (seedCommand) Allow(src cmd.Source) bool { return operator(src) }

func (seedCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	inWorld(tx, o, func(tx *world.Tx) {
		info, ok := manager.InfoForWorld(tx.World())
		if !ok {
			o.Error("This world is not managed by the server.")
			return
		}
		if info.BuiltIn {
			o.Printf("Seed: %d (shared by the built-in worlds).", builtInSeed)
			return
		}
		o.Printf("Seed: %d", info.Seed)
	})
}

// setWorldSpawnCommand is /setworldspawn.
type setWorldSpawnCommand struct {
	Position cmd.Optional[mgl64.Vec3] `cmd:"position"`
}

func (setWorldSpawnCommand) Allow(src cmd.Source) bool { return operator(src) }

func (c setWorldSpawnCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	inWorld(tx, o, func(tx *world.Tx) {
		pos := src.Position()
		if v, given := c.Position.Load(); given {
			pos = v
		}
		block := cube.PosFromVec3(pos)
		tx.World().SetSpawn(block)
		o.Printf("Set the world spawn to %v.", block)
	})
}

// gameRuleCommand is /gamerule.
//
// Dragonfly has no general game rule system, so this covers only the rules that
// map onto a real world setting. Offering the full vanilla list would mean
// accepting rules that silently do nothing.
type gameRuleCommand struct {
	Rule  gameRuleEnum              `cmd:"rule"`
	Value cmd.Optional[cmd.Varargs] `cmd:"value"`
}

func (gameRuleCommand) Allow(src cmd.Source) bool { return operator(src) }

func (c gameRuleCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	inWorld(tx, o, func(tx *world.Tx) {
		w := tx.World()
		rule := strings.ToLower(string(c.Rule))
		raw := strings.TrimSpace(string(c.Value.LoadOr("")))

		if raw == "" {
			switch rule {
			case "dodaylightcycle":
				o.Printf("dodaylightcycle = %v", w.TimeCycle())
			case "randomtickspeed":
				o.Print("randomtickspeed is set when the world is opened and cannot be read back.")
			default:
				o.Printf("%s cannot be read back on this server.", rule)
			}
			return
		}

		switch rule {
		case "dodaylightcycle":
			on, err := strconv.ParseBool(raw)
			if err != nil {
				o.Errorf("%q is not true or false.", raw)
				return
			}
			if on {
				w.StartTime()
			} else {
				w.StopTime()
			}
			o.Printf("dodaylightcycle = %v", on)
		case "doweathercycle":
			on, err := strconv.ParseBool(raw)
			if err != nil {
				o.Errorf("%q is not true or false.", raw)
				return
			}
			if on {
				w.StartWeatherCycle()
			} else {
				w.StopWeatherCycle()
			}
			o.Printf("doweathercycle = %v", on)
		case "randomtickspeed":
			n, err := strconv.Atoi(raw)
			if err != nil || n < 0 {
				o.Errorf("%q is not a tick speed.", raw)
				return
			}
			w.SetTickRange(n)
			o.Printf("randomtickspeed = %d", n)
		case "showcoordinates":
			on, err := strconv.ParseBool(raw)
			if err != nil {
				o.Errorf("%q is not true or false.", raw)
				return
			}
			p, isPlayer := playerSource(src)
			if !isPlayer {
				o.Error("showcoordinates applies per player, so run it in game.")
				return
			}
			if on {
				p.ShowCoordinates()
			} else {
				p.HideCoordinates()
			}
			o.Printf("showcoordinates = %v", on)
		default:
			o.Errorf("This server has no rule named %q.", rule)
		}
	})
}

// dayLockCommand is /daylock, also reachable as /alwaysday.
type dayLockCommand struct {
	Lock cmd.Optional[bool] `cmd:"lock"`
}

func (dayLockCommand) Allow(src cmd.Source) bool { return operator(src) }

func (c dayLockCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	inWorld(tx, o, func(tx *world.Tx) {
		w := tx.World()
		if lock := c.Lock.LoadOr(true); lock {
			w.SetTime(namedTimes["day"])
			w.StopTime()
			o.Print("The day is now locked.")
			return
		}
		w.StartTime()
		o.Print("The day is no longer locked.")
	})
}

// saveCommand is /save, writing the world to disk immediately.
type saveCommand struct{}

func (saveCommand) Allow(src cmd.Source) bool { return operator(src) }

func (saveCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	inWorld(tx, o, func(tx *world.Tx) {
		w := tx.World()
		// Save queues the write on the world's own goroutine, so it must not be
		// waited on from inside this transaction.
		go w.Save()
		o.Printf("Saving %s.", w.Name())
	})
}

func minIntC(a, b int) int {
	if a < b {
		return a
	}
	return b
}
