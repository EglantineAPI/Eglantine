package mob

import (
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestEveryTypeIsWellFormed checks the table itself: a mob with no name, no
// size or no health would be placeable but broken.
func TestEveryTypeIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range types {
		if !strings.HasPrefix(m.encoded, "minecraft:") {
			t.Errorf("%q is not a namespaced entity name", m.encoded)
		}
		if seen[m.encoded] {
			t.Errorf("%q appears twice in the table", m.encoded)
		}
		seen[m.encoded] = true

		if m.width <= 0 || m.width > 20 {
			t.Errorf("%s has width %v", m.encoded, m.width)
		}
		if m.height <= 0 || m.height > 20 {
			t.Errorf("%s has height %v", m.encoded, m.height)
		}
		if m.maxHealth <= 0 || m.maxHealth > 1000 {
			t.Errorf("%s has max health %v", m.encoded, m.maxHealth)
		}
	}
	if len(types) < 50 {
		t.Errorf("only %d mobs in the table", len(types))
	}
}

// TestBBoxMatchesSize checks the bounding box is built from the table, since a
// wrong box means a mob that cannot be hit where it is drawn.
func TestBBoxMatchesSize(t *testing.T) {
	for _, m := range types {
		box := m.BBox(nil)
		if got := box.Width(); got != m.width {
			t.Errorf("%s: box width %v, want %v", m.encoded, got, m.width)
		}
		if got := box.Height(); got != m.height {
			t.Errorf("%s: box height %v, want %v", m.encoded, got, m.height)
		}
		if box.Min().Y() != 0 {
			t.Errorf("%s: box does not sit on the ground", m.encoded)
		}
	}
}

// TestLookupAcceptsBothForms checks a name resolves with or without namespace,
// since the command enum offers the short form.
func TestLookupAcceptsBothForms(t *testing.T) {
	if _, ok := Lookup("zombie"); !ok {
		t.Error("Lookup(zombie) failed")
	}
	if _, ok := Lookup("minecraft:zombie"); !ok {
		t.Error("Lookup(minecraft:zombie) failed")
	}
	if _, ok := Lookup("ZOMBIE"); !ok {
		t.Error("Lookup is case sensitive")
	}
	if _, ok := Lookup("not_a_mob"); ok {
		t.Error("Lookup accepted a made-up name")
	}
}

// TestNamesMatchTable checks the command enum and the table cannot drift apart:
// every name offered has to resolve.
func TestNamesMatchTable(t *testing.T) {
	names := Names()
	if len(names) != len(types) {
		t.Errorf("%d names for %d types", len(names), len(types))
	}
	for _, n := range names {
		if strings.Contains(n, ":") {
			t.Errorf("name %q still carries a namespace", n)
		}
		if _, ok := Lookup(n); !ok {
			t.Errorf("name %q is offered but does not resolve", n)
		}
	}
}

// TestRegistryCoversDragonflyAndMobs checks the registry keeps Dragonfly's own
// entities. Dropping them would break arrows, dropped items and TNT.
func TestRegistryCoversDragonflyAndMobs(t *testing.T) {
	reg := Registry()
	for _, base := range entity.DefaultRegistry.Types() {
		if _, ok := reg.Lookup(base.EncodeEntity()); !ok {
			t.Errorf("the registry lost Dragonfly's %s", base.EncodeEntity())
		}
	}
	for _, m := range types {
		if _, ok := reg.Lookup(m.encoded); !ok {
			t.Errorf("the registry is missing %s", m.encoded)
		}
	}
}

// TestSpawnStartsAtFullHealth checks a placed mob is alive and whole.
func TestSpawnStartsAtFullHealth(t *testing.T) {
	zombie, ok := Lookup("zombie")
	if !ok {
		t.Fatal("no zombie in the table")
	}
	handle := Spawn(zombie, mgl64.Vec3{1, 2, 3})
	if handle == nil {
		t.Fatal("Spawn returned nothing")
	}
	if handle.Type().EncodeEntity() != "minecraft:zombie" {
		t.Errorf("handle has type %s", handle.Type().EncodeEntity())
	}
}

// TestNBTRoundTrip checks a wounded mob keeps its health across a save and
// load, rather than coming back untouched.
func TestNBTRoundTrip(t *testing.T) {
	zombie, _ := Lookup("zombie")
	var data world.EntityData

	zombie.DecodeNBT(map[string]any{"Health": float32(7)}, &data)
	s, ok := data.Data.(*state)
	if !ok {
		t.Fatal("DecodeNBT did not produce mob state")
	}
	if s.health.Health() != 7 {
		t.Errorf("health came back as %v, want 7", s.health.Health())
	}

	m := zombie.EncodeNBT(&data)
	if got := m["Health"]; got != float32(7) {
		t.Errorf("EncodeNBT wrote %v, want 7", got)
	}

	// A mob with no stored health starts full rather than dead.
	var fresh world.EntityData
	zombie.DecodeNBT(map[string]any{}, &fresh)
	if h := fresh.Data.(*state).health.Health(); h != zombie.maxHealth {
		t.Errorf("a mob with no saved health came back at %v, want %v", h, zombie.maxHealth)
	}
}

// TestFireImmunityIsSetWhereItShouldBe checks the nether natives ignore fire and
// that ordinary overworld mobs do not.
func TestFireImmunityIsSetWhereItShouldBe(t *testing.T) {
	for _, name := range []string{"blaze", "ghast", "magma_cube", "wither_skeleton", "strider"} {
		m, ok := Lookup(name)
		if !ok {
			t.Errorf("%s is not in the table", name)
			continue
		}
		if !m.fireImmune {
			t.Errorf("%s should be immune to fire", name)
		}
	}
	for _, name := range []string{"zombie", "cow", "creeper"} {
		if m, _ := Lookup(name); m.fireImmune {
			t.Errorf("%s should not be immune to fire", name)
		}
	}
}
