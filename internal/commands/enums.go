package commands

import (
	"sort"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// The command system builds an enum's options every time it describes a command
// to a joining player. Item and block names run into the thousands, so each
// list is built once and reused.
var (
	itemNamesOnce, blockNamesOnce sync.Once
	itemNames, blockNames         []string
)

// trimNamespace drops the "minecraft:" prefix, so commands read the way they do
// in game. Lookups accept either form.
func trimNamespace(name string) string { return strings.TrimPrefix(name, "minecraft:") }

// gameModeEnum completes the four game modes.
type gameModeEnum string

func (gameModeEnum) Type() string { return "GameMode" }

func (gameModeEnum) Options(cmd.Source) []string {
	return []string{"survival", "creative", "adventure", "spectator"}
}

// gameModes maps a name to the mode it selects. The numeric aliases match the
// ones vanilla accepts.
var gameModes = map[string]world.GameMode{
	"survival": world.GameModeSurvival, "s": world.GameModeSurvival, "0": world.GameModeSurvival,
	"creative": world.GameModeCreative, "c": world.GameModeCreative, "1": world.GameModeCreative,
	"adventure": world.GameModeAdventure, "a": world.GameModeAdventure, "2": world.GameModeAdventure,
	"spectator": world.GameModeSpectator, "spc": world.GameModeSpectator, "3": world.GameModeSpectator,
}

// difficultyEnum completes the four difficulties.
type difficultyEnum string

func (difficultyEnum) Type() string { return "Difficulty" }

func (difficultyEnum) Options(cmd.Source) []string {
	return []string{"peaceful", "easy", "normal", "hard"}
}

var difficulties = map[string]world.Difficulty{
	"peaceful": world.DifficultyPeaceful, "p": world.DifficultyPeaceful, "0": world.DifficultyPeaceful,
	"easy": world.DifficultyEasy, "e": world.DifficultyEasy, "1": world.DifficultyEasy,
	"normal": world.DifficultyNormal, "n": world.DifficultyNormal, "2": world.DifficultyNormal,
	"hard": world.DifficultyHard, "h": world.DifficultyHard, "3": world.DifficultyHard,
}

// itemEnum completes every registered item name.
type itemEnum string

func (itemEnum) Type() string { return "Item" }

func (itemEnum) Options(cmd.Source) []string {
	itemNamesOnce.Do(func() {
		seen := map[string]bool{}
		for _, it := range world.Items() {
			name, _ := it.EncodeItem()
			if short := trimNamespace(name); !seen[short] {
				seen[short] = true
				itemNames = append(itemNames, short)
			}
		}
		sort.Strings(itemNames)
	})
	return itemNames
}

// blockEnum completes every registered block name.
type blockEnum string

func (blockEnum) Type() string { return "Block" }

func (blockEnum) Options(cmd.Source) []string {
	blockNamesOnce.Do(func() {
		seen := map[string]bool{}
		for _, b := range world.Blocks() {
			name, _ := b.EncodeBlock()
			if short := trimNamespace(name); !seen[short] {
				seen[short] = true
				blockNames = append(blockNames, short)
			}
		}
		sort.Strings(blockNames)
	})
	return blockNames
}

// lookupItem resolves an item name with or without its namespace.
func lookupItem(name string) (world.Item, bool) {
	if it, ok := world.ItemByName(name, 0); ok {
		return it, true
	}
	return world.ItemByName("minecraft:"+trimNamespace(name), 0)
}

// lookupBlock resolves a block name with or without its namespace. Only the
// default state of a block can be named this way; block properties are not
// part of the command syntax here.
func lookupBlock(name string) (world.Block, bool) {
	if b, ok := world.BlockByName(name, nil); ok {
		return b, true
	}
	return world.BlockByName("minecraft:"+trimNamespace(name), nil)
}

// effectEntry pairs an effect with what it is called and whether it lasts.
type effectEntry struct {
	name    string
	typ     effect.Type
	lasting effect.LastingType
}

// effectList is the registered effect set. Dragonfly registers effects by
// numeric ID and does not name them, so the names live here, matching the ones
// the game uses.
var effectList = []effectEntry{
	{name: "speed", lasting: effect.Speed},
	{name: "slowness", lasting: effect.Slowness},
	{name: "haste", lasting: effect.Haste},
	{name: "mining_fatigue", lasting: effect.MiningFatigue},
	{name: "strength", lasting: effect.Strength},
	{name: "instant_health", typ: effect.InstantHealth},
	{name: "instant_damage", typ: effect.InstantDamage},
	{name: "jump_boost", lasting: effect.JumpBoost},
	{name: "nausea", lasting: effect.Nausea},
	{name: "regeneration", lasting: effect.Regeneration},
	{name: "resistance", lasting: effect.Resistance},
	{name: "fire_resistance", lasting: effect.FireResistance},
	{name: "water_breathing", lasting: effect.WaterBreathing},
	{name: "invisibility", lasting: effect.Invisibility},
	{name: "blindness", lasting: effect.Blindness},
	{name: "night_vision", lasting: effect.NightVision},
	{name: "hunger", lasting: effect.Hunger},
	{name: "weakness", lasting: effect.Weakness},
	{name: "poison", lasting: effect.Poison},
	{name: "wither", lasting: effect.Wither},
	{name: "health_boost", lasting: effect.HealthBoost},
	{name: "absorption", lasting: effect.Absorption},
	{name: "saturation", lasting: effect.Saturation},
	{name: "levitation", lasting: effect.Levitation},
	{name: "fatal_poison", lasting: effect.FatalPoison},
	{name: "conduit_power", lasting: effect.ConduitPower},
	{name: "slow_falling", lasting: effect.SlowFalling},
	{name: "darkness", lasting: effect.Darkness},
}

// effectEnum completes the effect names, plus "clear" to remove them all.
type effectEnum string

func (effectEnum) Type() string { return "Effect" }

func (effectEnum) Options(cmd.Source) []string {
	names := make([]string, 0, len(effectList)+1)
	names = append(names, "clear")
	for _, e := range effectList {
		names = append(names, e.name)
	}
	sort.Strings(names)
	return names
}

// lookupEffect resolves an effect name.
func lookupEffect(name string) (effectEntry, bool) {
	name = trimNamespace(strings.ToLower(name))
	for _, e := range effectList {
		if e.name == name {
			return e, true
		}
	}
	return effectEntry{}, false
}

// enchantmentEnum completes the enchantment names.
type enchantmentEnum string

func (enchantmentEnum) Type() string { return "Enchantment" }

func (enchantmentEnum) Options(cmd.Source) []string {
	names := make([]string, 0, 40)
	for _, e := range item.Enchantments() {
		names = append(names, enchantmentName(e))
	}
	sort.Strings(names)
	return names
}

// enchantmentName is the command-line form of an enchantment: its display name
// lowercased with spaces turned into underscores, as the game writes it.
func enchantmentName(e item.EnchantmentType) string {
	return strings.ReplaceAll(strings.ToLower(e.Name()), " ", "_")
}

// lookupEnchantment resolves an enchantment name.
func lookupEnchantment(name string) (item.EnchantmentType, bool) {
	name = trimNamespace(strings.ToLower(name))
	for _, e := range item.Enchantments() {
		if enchantmentName(e) == name {
			return e, true
		}
	}
	return nil, false
}

// weatherEnum completes the weather states.
type weatherEnum string

func (weatherEnum) Type() string { return "Weather" }

func (weatherEnum) Options(cmd.Source) []string {
	return []string{"clear", "rain", "thunder"}
}

// timeSpecEnum completes the named times of day accepted by /time set.
type timeSpecEnum string

func (timeSpecEnum) Type() string { return "TimeSpec" }

func (timeSpecEnum) Options(cmd.Source) []string {
	return []string{"day", "night", "noon", "midnight", "sunrise", "sunset"}
}

// namedTimes are the tick values the named times map to, matching vanilla.
var namedTimes = map[string]int{
	"day": 1000, "noon": 6000, "sunset": 12000,
	"night": 13000, "midnight": 18000, "sunrise": 23000,
}

// titleActionEnum completes what /title can do.
type titleActionEnum string

func (titleActionEnum) Type() string { return "TitleAction" }

func (titleActionEnum) Options(cmd.Source) []string {
	return []string{"title", "subtitle", "actionbar", "clear", "reset"}
}

// gameRuleEnum completes the rules this server actually has. Dragonfly has no
// general game rule system, so only the handful backed by a real world setting
// are offered rather than listing rules that would silently do nothing.
type gameRuleEnum string

func (gameRuleEnum) Type() string { return "GameRule" }

func (gameRuleEnum) Options(cmd.Source) []string {
	return []string{"dodaylightcycle", "doweathercycle", "randomtickspeed", "showcoordinates"}
}
