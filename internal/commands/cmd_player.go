package commands

import (
	"strconv"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/title"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"

	"server/internal/mob"
)

// targets resolves an optional target list to players. When no target is given
// the source itself is used, which is how every vanilla command behaves when a
// player runs it without naming anyone.
func targets(opt cmd.Optional[[]cmd.Target], src cmd.Source, o *cmd.Output) []*player.Player {
	list, given := opt.Load()
	if !given {
		p, ok := src.(*player.Player)
		if !ok {
			o.Error("Name a player when running this from the console.")
			return nil
		}
		return []*player.Player{p}
	}
	var out []*player.Player
	for _, t := range list {
		if p, ok := t.(*player.Player); ok {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		o.Error("No player matched.")
	}
	return out
}

// gameModeCommand is /gamemode.
type gameModeCommand struct {
	Mode    gameModeEnum               `cmd:"gamemode"`
	Targets cmd.Optional[[]cmd.Target] `cmd:"player"`
}

func (gameModeCommand) Allow(src cmd.Source) bool { return operator(src) }

func (c gameModeCommand) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	mode, ok := gameModes[strings.ToLower(string(c.Mode))]
	if !ok {
		o.Errorf("Unknown game mode %q.", string(c.Mode))
		return
	}
	for _, p := range targets(c.Targets, src, o) {
		p.SetGameMode(mode)
		o.Printf("Set %s's game mode to %s.", p.Name(), string(c.Mode))
	}
}

// teleportToPlayer is /tp <player>, moving the source onto another player.
type teleportToPlayer struct {
	Destination []cmd.Target `cmd:"destination"`
}

func (teleportToPlayer) Allow(src cmd.Source) bool { return operator(src) }

func (c teleportToPlayer) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		o.Error("Name the player to move when running this from the console.")
		return
	}
	if len(c.Destination) == 0 {
		o.Error("No player matched.")
		return
	}
	p.Teleport(c.Destination[0].Position())
	o.Printf("Teleported %s.", p.Name())
}

// teleportToPos is /tp <x y z> [player].
type teleportToPos struct {
	Position mgl64.Vec3                 `cmd:"position"`
	Targets  cmd.Optional[[]cmd.Target] `cmd:"player"`
}

func (teleportToPos) Allow(src cmd.Source) bool { return operator(src) }

func (c teleportToPos) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	for _, p := range targets(c.Targets, src, o) {
		p.Teleport(c.Position)
		o.Printf("Teleported %s to %.0f %.0f %.0f.", p.Name(), c.Position[0], c.Position[1], c.Position[2])
	}
}

// killCommand is /kill. It only reaches players; entities are out of scope
// until the server has them.
type killCommand struct {
	Targets cmd.Optional[[]cmd.Target] `cmd:"player"`
}

func (killCommand) Allow(src cmd.Source) bool { return operator(src) }

func (c killCommand) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	for _, p := range targets(c.Targets, src, o) {
		// Void damage ignores armour, resistance and totems, which is what
		// makes /kill final rather than survivable.
		p.Hurt(p.MaxHealth()*2, entity.VoidDamageSource{})
		o.Printf("Killed %s.", p.Name())
	}
}

// giveCommand is /give.
type giveCommand struct {
	Targets []cmd.Target      `cmd:"player"`
	Item    itemEnum          `cmd:"item"`
	Count   cmd.Optional[int] `cmd:"amount"`
}

func (giveCommand) Allow(src cmd.Source) bool { return operator(src) }

func (c giveCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	it, ok := lookupItem(string(c.Item))
	if !ok {
		o.Errorf("Unknown item %q.", string(c.Item))
		return
	}
	count := c.Count.LoadOr(1)
	if count < 1 || count > 32767 {
		o.Error("Amount must be between 1 and 32767.")
		return
	}
	for _, t := range c.Targets {
		p, ok := t.(*player.Player)
		if !ok {
			continue
		}
		// AddItem reports how much fitted; the rest is dropped at the player's
		// feet the way vanilla does when an inventory is full.
		stack := item.NewStack(it, count)
		if n, err := p.Inventory().AddItem(stack); err != nil {
			p.Drop(stack.Grow(n - count))
		}
		o.Printf("Gave %d %s to %s.", count, string(c.Item), p.Name())
	}
}

// clearCommand is /clear, emptying a player's inventory.
type clearCommand struct {
	Targets cmd.Optional[[]cmd.Target] `cmd:"player"`
}

func (clearCommand) Allow(src cmd.Source) bool { return operator(src) }

func (c clearCommand) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	for _, p := range targets(c.Targets, src, o) {
		cleared := 0
		for slot, st := range p.Inventory().Slots() {
			if st.Empty() {
				continue
			}
			cleared += st.Count()
			_ = p.Inventory().SetItem(slot, item.Stack{})
		}
		o.Printf("Cleared %d items from %s.", cleared, p.Name())
	}
}

// experienceCommand is /xp, also reachable as /experience.
type experienceCommand struct {
	Amount  string                     `cmd:"amount"`
	Targets cmd.Optional[[]cmd.Target] `cmd:"player"`
}

func (experienceCommand) Allow(src cmd.Source) bool { return operator(src) }

func (c experienceCommand) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	// Vanilla writes levels as a trailing L, as in "30L"; a bare number is
	// experience points.
	raw, levels := strings.TrimSuffix(strings.ToUpper(c.Amount), "L"), strings.HasSuffix(strings.ToUpper(c.Amount), "L")
	n, err := strconv.Atoi(raw)
	if err != nil {
		o.Errorf("%q is not a number of experience.", c.Amount)
		return
	}
	for _, p := range targets(c.Targets, src, o) {
		switch {
		case levels:
			p.SetExperienceLevel(maxIntC(0, p.ExperienceLevel()+n))
		case n >= 0:
			p.AddExperience(n)
		default:
			p.RemoveExperience(-n)
		}
		o.Printf("Gave %s %d experience%s.", p.Name(), n, map[bool]string{true: " levels"}[levels])
	}
}

// effectCommand is /effect, adding an effect or clearing them all.
type effectCommand struct {
	Targets   []cmd.Target      `cmd:"player"`
	Effect    effectEnum        `cmd:"effect"`
	Seconds   cmd.Optional[int] `cmd:"seconds"`
	Amplifier cmd.Optional[int] `cmd:"amplifier"`
}

func (effectCommand) Allow(src cmd.Source) bool { return operator(src) }

func (c effectCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	name := strings.ToLower(string(c.Effect))
	if name == "clear" {
		for _, t := range c.Targets {
			if p, ok := t.(*player.Player); ok {
				for _, e := range p.Effects() {
					p.RemoveEffect(e.Type())
				}
				o.Printf("Cleared all effects from %s.", p.Name())
			}
		}
		return
	}

	entry, ok := lookupEffect(name)
	if !ok {
		o.Errorf("Unknown effect %q.", name)
		return
	}
	// Vanilla counts the amplifier from zero, so level 1 is amplifier 0.
	level := c.Amplifier.LoadOr(0) + 1
	seconds := c.Seconds.LoadOr(30)
	if seconds < 0 {
		o.Error("Duration cannot be negative.")
		return
	}

	for _, t := range c.Targets {
		p, ok := t.(*player.Player)
		if !ok {
			continue
		}
		if entry.lasting == nil {
			// Instant effects apply once and have no duration.
			p.AddEffect(effect.NewInstant(entry.typ, level))
		} else {
			p.AddEffect(effect.New(entry.lasting, level, time.Duration(seconds)*time.Second))
		}
		o.Printf("Applied %s to %s.", entry.name, p.Name())
	}
}

// enchantCommand is /enchant, enchanting whatever the player is holding.
type enchantCommand struct {
	Targets     []cmd.Target      `cmd:"player"`
	Enchantment enchantmentEnum   `cmd:"enchantment"`
	Level       cmd.Optional[int] `cmd:"level"`
}

func (enchantCommand) Allow(src cmd.Source) bool { return operator(src) }

func (c enchantCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	e, ok := lookupEnchantment(string(c.Enchantment))
	if !ok {
		o.Errorf("Unknown enchantment %q.", string(c.Enchantment))
		return
	}
	level := c.Level.LoadOr(1)
	if level < 1 || level > e.MaxLevel() {
		o.Errorf("%s only goes up to level %d.", e.Name(), e.MaxLevel())
		return
	}
	for _, t := range c.Targets {
		p, ok := t.(*player.Player)
		if !ok {
			continue
		}
		held, _ := p.HeldItems()
		if held.Empty() {
			o.Errorf("%s is not holding anything.", p.Name())
			continue
		}
		if !e.CompatibleWithItem(held.Item()) {
			o.Errorf("%s cannot be enchanted with %s.", p.Name(), e.Name())
			continue
		}
		p.SetHeldItems(held.WithEnchantments(item.NewEnchantment(e, level)), item.Stack{})
		o.Printf("Enchanted %s's item with %s %d.", p.Name(), e.Name(), level)
	}
}

// titleCommand is /title.
type titleCommand struct {
	Targets []cmd.Target              `cmd:"player"`
	Action  titleActionEnum           `cmd:"action"`
	Text    cmd.Optional[cmd.Varargs] `cmd:"text"`
}

func (titleCommand) Allow(src cmd.Source) bool { return operator(src) }

func (c titleCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	action := strings.ToLower(string(c.Action))
	text := string(c.Text.LoadOr(""))
	if text == "" && action != "clear" && action != "reset" {
		o.Error("Give some text to show.")
		return
	}
	for _, t := range c.Targets {
		p, ok := t.(*player.Player)
		if !ok {
			continue
		}
		switch action {
		case "title":
			p.SendTitle(title.New(text))
		case "subtitle":
			// A subtitle needs a title to sit under, so an empty one is sent
			// with it rather than the subtitle silently not appearing.
			p.SendTitle(title.New("").WithSubtitle(text))
		case "actionbar":
			p.SendTitle(title.New("").WithActionText(text))
		case "clear", "reset":
			p.SendTitle(title.New("").WithDuration(0))
		}
		o.Printf("Sent a title to %s.", p.Name())
	}
}

// spawnPointCommand is /spawnpoint, setting where a player respawns.
type spawnPointCommand struct {
	Targets  cmd.Optional[[]cmd.Target] `cmd:"player"`
	Position cmd.Optional[mgl64.Vec3]   `cmd:"position"`
}

func (spawnPointCommand) Allow(src cmd.Source) bool { return operator(src) }

func (c spawnPointCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	if tx == nil {
		o.Error("The console is not in a world.")
		return
	}
	for _, p := range targets(c.Targets, src, o) {
		pos := p.Position()
		if v, ok := c.Position.Load(); ok {
			pos = v
		}
		block := cube.PosFromVec3(pos)
		tx.World().SetPlayerSpawn(p.UUID(), block)
		o.Printf("Set %s's spawn point to %v.", p.Name(), block)
	}
}

func maxIntC(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// summonCommand places a mob in the world.
//
// The mobs this server has do not spawn on their own and have no AI, so this is
// the only way one appears at all.
type summonCommand struct {
	Mob      mobEnum                  `cmd:"mob"`
	Position cmd.Optional[mgl64.Vec3] `cmd:"position"`
	NameTag  cmd.Optional[string]     `cmd:"nametag"`
}

func (summonCommand) Allow(src cmd.Source) bool { return operator(src) }

func (c summonCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	t, ok := mob.Lookup(string(c.Mob))
	if !ok {
		o.Errorf("Unknown mob %q.", string(c.Mob))
		return
	}
	pos, given := c.Position.Load()
	if !given {
		p, isPlayer := src.(*player.Player)
		if !isPlayer {
			o.Error("Give a position when running this from the console.")
			return
		}
		pos = p.Position()
	}

	place := func(tx *world.Tx) {
		handle := mob.Spawn(t, pos)
		e := tx.AddEntity(handle)
		if tag, ok := c.NameTag.Load(); ok && tag != "" {
			if named, ok := e.(interface {
				SetNameTag(string)
				SetAlwaysShowNameTag(bool)
			}); ok {
				named.SetNameTag(tag)
				named.SetAlwaysShowNameTag(true)
			}
		}
	}
	inWorld(tx, o, place)
	o.Printf("Summoned %s at %.0f %.0f %.0f.", t.Name(), pos[0], pos[1], pos[2])
}
