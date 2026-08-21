package commands

import (
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"

	"server/internal/mob"
)

// TestMain finalizes the block registry, which the block name enum needs, and
// registers the commands once. cmd.Register writes to a package-level registry,
// so registering more than once would stack duplicates.
func TestMain(m *testing.M) {
	world.DefaultBlockRegistry.Finalize()
	Register(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, 0, nil)
	os.Exit(m.Run())
}

// TestEveryCommandRegisters is the check that every Runnable is well formed.
// cmd.New validates the parameter types of each struct by reflection and panics
// on anything it cannot parse, so reaching this point at all means all of them
// are valid; the names confirm nothing was left out of the wiring.
func TestEveryCommandRegisters(t *testing.T) {
	want := []string{
		"clear", "clone", "daylock", "deop", "difficulty", "effect", "enchant",
		"experience", "fill", "gamemode", "gamerule", "give", "help", "kick",
		"kill", "list", "me", "op", "save", "say", "seed", "setblock",
		"setworldspawn", "spawnpoint", "stop", "summon", "teleport", "tell", "time",
		"title", "transfer", "weather", "world",
	}
	registered := map[string]bool{}
	for _, c := range cmd.Commands() {
		registered[c.Name()] = true
	}
	for _, name := range want {
		if !registered[name] {
			t.Errorf("/%s is not registered", name)
		}
	}
}

// TestAliasesResolve checks the shorthands players actually type.
func TestAliasesResolve(t *testing.T) {
	for alias, want := range map[string]string{
		"tp": "teleport", "gm": "gamemode", "xp": "experience",
		"msg": "tell", "w": "tell", "alwaysday": "daylock", "?": "help",
	} {
		c, ok := cmd.ByAlias(alias)
		if !ok {
			t.Errorf("alias %q resolves to nothing", alias)
			continue
		}
		if c.Name() != want {
			t.Errorf("alias %q resolves to /%s, want /%s", alias, c.Name(), want)
		}
	}
}

// enums is every enum the commands use.
func enums() []cmd.Enum {
	return []cmd.Enum{
		gameModeEnum(""), difficultyEnum(""), itemEnum(""), blockEnum(""),
		effectEnum(""), enchantmentEnum(""), weatherEnum(""), timeSpecEnum(""),
		mobEnum(""),
		titleActionEnum(""), gameRuleEnum(""), worldName(""), generatorKind(""),
	}
}

// TestEnumTypesAreUnique guards the client's autocomplete. Two enums sharing a
// type name collide in the available commands packet, and the client then
// completes one of them with the other's values.
func TestEnumTypesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range enums() {
		name := e.Type()
		if seen[name] {
			t.Errorf("enum type %q is used twice", name)
		}
		seen[name] = true
	}
}

// TestEnumsHaveOptions checks no enum offers an empty list, which would leave a
// parameter that can never be satisfied.
func TestEnumsHaveOptions(t *testing.T) {
	for _, e := range enums() {
		// The world name enum is empty without a manager, which this test does
		// not build; every other enum is static.
		if e.Type() == "world" {
			continue
		}
		if opts := e.Options(nil); len(opts) == 0 {
			t.Errorf("enum %q offers no options", e.Type())
		}
	}
}

// TestItemAndBlockEnums checks the two big enums are populated and sorted, so
// the client shows them in a usable order.
func TestItemAndBlockEnums(t *testing.T) {
	for _, e := range []cmd.Enum{itemEnum(""), blockEnum("")} {
		opts := e.Options(nil)
		if len(opts) < 100 {
			t.Errorf("%s enum has only %d entries", e.Type(), len(opts))
		}
		if !sort.StringsAreSorted(opts) {
			t.Errorf("%s enum is not sorted", e.Type())
		}
		for _, name := range opts {
			if strings.Contains(name, ":") {
				t.Errorf("%s enum entry %q still carries a namespace", e.Type(), name)
				break
			}
		}
	}
}

// TestLookupsAcceptBothForms checks a name resolves with or without the
// namespace, since the enum offers the short form but players paste the long one.
func TestLookupsAcceptBothForms(t *testing.T) {
	if _, ok := lookupItem("stone"); !ok {
		t.Error("lookupItem(stone) failed")
	}
	if _, ok := lookupItem("minecraft:stone"); !ok {
		t.Error("lookupItem(minecraft:stone) failed")
	}
	if _, ok := lookupBlock("dirt"); !ok {
		t.Error("lookupBlock(dirt) failed")
	}
	if _, ok := lookupBlock("minecraft:dirt"); !ok {
		t.Error("lookupBlock(minecraft:dirt) failed")
	}
	if _, ok := lookupItem("not_a_real_item"); ok {
		t.Error("lookupItem accepted a made-up name")
	}
}

// TestEveryEffectResolves checks each name the effect enum offers can be looked
// back up, so nothing is completable but unusable.
func TestEveryEffectResolves(t *testing.T) {
	for _, name := range effectEnum("").Options(nil) {
		if name == "clear" {
			continue
		}
		entry, ok := lookupEffect(name)
		if !ok {
			t.Errorf("effect %q completes but does not resolve", name)
			continue
		}
		// Every effect is either lasting or instant; one that is neither would
		// silently do nothing when applied.
		if entry.lasting == nil && entry.typ == nil {
			t.Errorf("effect %q has neither a lasting nor an instant type", name)
		}
	}
}

// TestEveryMobResolves checks each mob the enum offers can be looked back up.
func TestEveryMobResolves(t *testing.T) {
	opts := mobEnum("").Options(nil)
	if len(opts) < 50 {
		t.Errorf("only %d mobs are offered", len(opts))
	}
	for _, name := range opts {
		if _, ok := mob.Lookup(name); !ok {
			t.Errorf("mob %q completes but does not resolve", name)
		}
	}
}

// TestEveryEnchantmentResolves is the same check for enchantments.
func TestEveryEnchantmentResolves(t *testing.T) {
	opts := enchantmentEnum("").Options(nil)
	if len(opts) == 0 {
		t.Fatal("no enchantments are offered")
	}
	for _, name := range opts {
		if _, ok := lookupEnchantment(name); !ok {
			t.Errorf("enchantment %q completes but does not resolve", name)
		}
	}
}

// TestModeNamesCoverEnum checks every game mode and difficulty the client can
// complete is one the command actually accepts.
func TestModeNamesCoverEnum(t *testing.T) {
	for _, name := range gameModeEnum("").Options(nil) {
		if _, ok := gameModes[name]; !ok {
			t.Errorf("game mode %q completes but is not accepted", name)
		}
	}
	for _, name := range difficultyEnum("").Options(nil) {
		if _, ok := difficulties[name]; !ok {
			t.Errorf("difficulty %q completes but is not accepted", name)
		}
	}
	for _, name := range timeSpecEnum("").Options(nil) {
		if _, ok := namedTimes[name]; !ok {
			t.Errorf("time %q completes but is not accepted", name)
		}
	}
}
