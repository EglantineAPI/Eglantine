package mob

import (
	"sort"
	"strings"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// The mob table. Dimensions and health are vanilla's, so a mob occupies the
// space its model does and takes the number of hits it should. They are not
// read from game data — Dragonfly ships none for entities — so a value here
// being a little off is possible and is worth checking in game.
var types = []Type{
	// Hostile, overworld.
	{encoded: "minecraft:zombie", width: 0.6, height: 1.95, maxHealth: 20},
	{encoded: "minecraft:zombie_villager", width: 0.6, height: 1.95, maxHealth: 20},
	{encoded: "minecraft:husk", width: 0.6, height: 1.95, maxHealth: 20},
	{encoded: "minecraft:drowned", width: 0.6, height: 1.95, maxHealth: 20},
	{encoded: "minecraft:skeleton", width: 0.6, height: 1.99, maxHealth: 20},
	{encoded: "minecraft:stray", width: 0.6, height: 1.99, maxHealth: 20},
	{encoded: "minecraft:bogged", width: 0.6, height: 1.99, maxHealth: 16},
	{encoded: "minecraft:creeper", width: 0.6, height: 1.7, maxHealth: 20},
	{encoded: "minecraft:spider", width: 1.4, height: 0.9, maxHealth: 16},
	{encoded: "minecraft:cave_spider", width: 0.7, height: 0.5, maxHealth: 12},
	{encoded: "minecraft:enderman", width: 0.6, height: 2.9, maxHealth: 40},
	{encoded: "minecraft:endermite", width: 0.4, height: 0.3, maxHealth: 8},
	{encoded: "minecraft:silverfish", width: 0.4, height: 0.3, maxHealth: 8},
	{encoded: "minecraft:witch", width: 0.6, height: 1.95, maxHealth: 26},
	{encoded: "minecraft:slime", width: 2.08, height: 2.08, maxHealth: 16},
	{encoded: "minecraft:phantom", width: 0.9, height: 0.5, maxHealth: 20},
	{encoded: "minecraft:guardian", width: 0.85, height: 0.85, maxHealth: 30},
	{encoded: "minecraft:elder_guardian", width: 1.99, height: 1.99, maxHealth: 80},
	{encoded: "minecraft:shulker", width: 1, height: 1, maxHealth: 30},
	{encoded: "minecraft:warden", width: 0.9, height: 2.9, maxHealth: 500},
	{encoded: "minecraft:breeze", width: 0.6, height: 1.77, maxHealth: 30},
	{encoded: "minecraft:creaking", width: 0.9, height: 2.7, maxHealth: 1},

	// Illagers.
	{encoded: "minecraft:pillager", width: 0.6, height: 1.95, maxHealth: 24},
	{encoded: "minecraft:vindicator", width: 0.6, height: 1.95, maxHealth: 24},
	{encoded: "minecraft:evocation_illager", width: 0.6, height: 1.95, maxHealth: 24},
	{encoded: "minecraft:vex", width: 0.4, height: 0.8, maxHealth: 14},
	{encoded: "minecraft:ravager", width: 1.95, height: 2.2, maxHealth: 100},

	// Nether. Everything native to it ignores fire.
	{encoded: "minecraft:blaze", width: 0.6, height: 1.8, maxHealth: 20, fireImmune: true},
	{encoded: "minecraft:ghast", width: 4, height: 4, maxHealth: 10, fireImmune: true},
	{encoded: "minecraft:magma_cube", width: 2.08, height: 2.08, maxHealth: 16, fireImmune: true},
	{encoded: "minecraft:zombie_pigman", width: 0.6, height: 1.95, maxHealth: 20, fireImmune: true},
	{encoded: "minecraft:piglin", width: 0.6, height: 1.95, maxHealth: 16},
	{encoded: "minecraft:piglin_brute", width: 0.6, height: 1.95, maxHealth: 50},
	{encoded: "minecraft:hoglin", width: 1.4, height: 1.4, maxHealth: 40},
	{encoded: "minecraft:zoglin", width: 1.4, height: 1.4, maxHealth: 40},
	{encoded: "minecraft:strider", width: 0.9, height: 1.7, maxHealth: 20, fireImmune: true},
	{encoded: "minecraft:wither_skeleton", width: 0.7, height: 2.4, maxHealth: 20, fireImmune: true},

	// Passive, overworld.
	{encoded: "minecraft:cow", width: 0.9, height: 1.4, maxHealth: 10},
	{encoded: "minecraft:mooshroom", width: 0.9, height: 1.4, maxHealth: 10},
	{encoded: "minecraft:pig", width: 0.9, height: 0.9, maxHealth: 10},
	{encoded: "minecraft:sheep", width: 0.9, height: 1.3, maxHealth: 8},
	{encoded: "minecraft:chicken", width: 0.4, height: 0.7, maxHealth: 4},
	{encoded: "minecraft:rabbit", width: 0.4, height: 0.5, maxHealth: 3},
	{encoded: "minecraft:horse", width: 1.4, height: 1.6, maxHealth: 30},
	{encoded: "minecraft:donkey", width: 1.4, height: 1.6, maxHealth: 30},
	{encoded: "minecraft:mule", width: 1.4, height: 1.6, maxHealth: 30},
	{encoded: "minecraft:llama", width: 0.9, height: 1.87, maxHealth: 30},
	{encoded: "minecraft:wolf", width: 0.6, height: 0.85, maxHealth: 8},
	{encoded: "minecraft:cat", width: 0.6, height: 0.7, maxHealth: 10},
	{encoded: "minecraft:ocelot", width: 0.6, height: 0.7, maxHealth: 10},
	{encoded: "minecraft:fox", width: 0.6, height: 0.7, maxHealth: 10},
	{encoded: "minecraft:panda", width: 1.3, height: 1.25, maxHealth: 20},
	{encoded: "minecraft:polar_bear", width: 1.4, height: 1.4, maxHealth: 30},
	{encoded: "minecraft:goat", width: 0.9, height: 1.3, maxHealth: 10},
	{encoded: "minecraft:camel", width: 1.7, height: 2.375, maxHealth: 32},
	{encoded: "minecraft:sniffer", width: 1.9, height: 1.75, maxHealth: 14},
	{encoded: "minecraft:armadillo", width: 0.7, height: 0.65, maxHealth: 12},
	{encoded: "minecraft:bee", width: 0.7, height: 0.6, maxHealth: 10},
	{encoded: "minecraft:bat", width: 0.5, height: 0.9, maxHealth: 6},
	{encoded: "minecraft:parrot", width: 0.5, height: 0.9, maxHealth: 6},
	{encoded: "minecraft:frog", width: 0.5, height: 0.5, maxHealth: 10},
	{encoded: "minecraft:tadpole", width: 0.4, height: 0.3, maxHealth: 6},
	{encoded: "minecraft:turtle", width: 1.2, height: 0.4, maxHealth: 30},
	{encoded: "minecraft:allay", width: 0.35, height: 0.6, maxHealth: 20},

	// Aquatic.
	{encoded: "minecraft:squid", width: 0.8, height: 0.8, maxHealth: 10},
	{encoded: "minecraft:glow_squid", width: 0.8, height: 0.8, maxHealth: 10},
	{encoded: "minecraft:dolphin", width: 0.9, height: 0.6, maxHealth: 10},
	{encoded: "minecraft:cod", width: 0.5, height: 0.3, maxHealth: 3},
	{encoded: "minecraft:salmon", width: 0.7, height: 0.4, maxHealth: 3},
	{encoded: "minecraft:tropicalfish", width: 0.5, height: 0.4, maxHealth: 3},
	{encoded: "minecraft:pufferfish", width: 0.7, height: 0.7, maxHealth: 3},
	{encoded: "minecraft:axolotl", width: 0.75, height: 0.42, maxHealth: 14},

	// Villagers and constructs.
	{encoded: "minecraft:villager_v2", width: 0.6, height: 1.95, maxHealth: 20},
	{encoded: "minecraft:wandering_trader", width: 0.6, height: 1.95, maxHealth: 20},
	{encoded: "minecraft:trader_llama", width: 0.9, height: 1.87, maxHealth: 15},
	{encoded: "minecraft:iron_golem", width: 1.4, height: 2.7, maxHealth: 100},
	{encoded: "minecraft:snow_golem", width: 0.7, height: 1.9, maxHealth: 4},

	// Undead mounts.
	{encoded: "minecraft:skeleton_horse", width: 1.4, height: 1.6, maxHealth: 15},
	{encoded: "minecraft:zombie_horse", width: 1.4, height: 1.6, maxHealth: 15},
}

// byName indexes the table by its short name, without the namespace.
var byName = func() map[string]Type {
	m := make(map[string]Type, len(types))
	for _, t := range types {
		m[strings.TrimPrefix(t.encoded, "minecraft:")] = t
	}
	return m
}()

// Types returns every mob type, for registering with a world.
func Types() []world.EntityType {
	out := make([]world.EntityType, 0, len(types))
	for _, t := range types {
		out = append(out, t)
	}
	return out
}

// Names returns every mob name without its namespace, sorted. Commands use this
// as an enum so the names complete as they are typed.
func Names() []string {
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Lookup resolves a mob name, with or without its namespace.
func Lookup(name string) (Type, bool) {
	t, ok := byName[strings.TrimPrefix(strings.ToLower(name), "minecraft:")]
	return t, ok
}

// Registry returns an entity registry holding both Dragonfly's own entity types
// and every mob here. A world built without it cannot open a saved mob, so all
// worlds have to share one registry.
func Registry() world.EntityRegistry {
	base := entity.DefaultRegistry
	return base.Config().New(append(base.Types(), Types()...))
}

// Spawn creates a mob of the type passed at a position. The caller adds the
// returned handle to a world.
//
// Nothing calls this during world generation: mobs never appear on their own.
func Spawn(t Type, pos mgl64.Vec3) *world.EntityHandle {
	opts := world.EntitySpawnOpts{Position: pos}
	return opts.New(t, spawnConf{t: t})
}

// spawnConf is a world.EntityConfig that seeds a new mob with full health.
type spawnConf struct{ t Type }

func (c spawnConf) Apply(data *world.EntityData) {
	data.Data = newState(c.t, c.t.maxHealth)
}
