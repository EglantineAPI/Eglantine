package gen

import (
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// TestMain finalizes the default block registry. Resolving a block to a runtime
// ID panics until that happens. In the running server it is done by
// server.Config.New; nothing here constructs a Server, so the tests do it
// themselves.
func TestMain(m *testing.M) {
	world.DefaultBlockRegistry.Finalize()
	os.Exit(m.Run())
}

// newChunk allocates an empty chunk in the dimension of the Kind passed.
func newChunk(k Kind) *chunk.Chunk {
	return chunk.New(world.DefaultBlockRegistry, k.Dimension().Range())
}

// histogram generates a square of chunks and counts every runtime ID placed.
func histogram(t *testing.T, k Kind, seed int64, radius int32) map[uint32]int {
	t.Helper()
	g, err := k.New(seed)
	if err != nil {
		t.Fatalf("New(%q): %v", k, err)
	}
	counts := map[uint32]int{}
	for cx := -radius; cx <= radius; cx++ {
		for cz := -radius; cz <= radius; cz++ {
			c := newChunk(k)
			g.GenerateChunk(world.ChunkPos{cx, cz}, c)
			r := c.Range()
			for x := range uint8(16) {
				for z := range uint8(16) {
					for y := int16(r.Min()); y <= int16(r.Max()); y++ {
						counts[c.Block(x, y, z, 0)]++
					}
				}
			}
		}
	}
	return counts
}

// TestOverworldIsSurvivable checks that the overworld actually contains the
// things a player needs, rather than merely generating without panicking.
func TestOverworldIsSurvivable(t *testing.T) {
	o := NewOverworld(1234)
	counts := histogram(t, KindOverworld, 1234, 2)

	for _, tc := range []struct {
		name string
		rid  uint32
		min  int
	}{
		{"grass", o.grass, 1000},
		{"stone", o.stone, 10000},
		{"deepslate", o.deepslate, 10000},
		{"dirt", o.dirt, 1000},
		{"water", o.water, 100},
		{"bedrock", o.bedrock, 100},
		{"logs", o.log, 20},
		{"leaves", o.leaves, 100},
		{"coal ore", o.oreStone[0], 20},
		{"iron ore", o.oreStone[1], 20},
		{"diamond ore", o.oreDeep[3], 1},
		{"lava", o.lava, 1},
	} {
		if got := counts[tc.rid]; got < tc.min {
			t.Errorf("%s: got %d blocks, want at least %d", tc.name, got, tc.min)
		}
	}

	// Caves must actually hollow the ground out, otherwise the world is a
	// solid brick with ores buried unreachably inside it.
	if air := counts[o.air]; air < 50000 {
		t.Errorf("air: got %d, want at least 50000 (caves are not carving)", air)
	}
}

// TestSurfaceIsReachable checks the heightmap stays inside the build range and
// varies, so terrain is neither flat nor clipped against the world ceiling.
func TestSurfaceIsReachable(t *testing.T) {
	o := NewOverworld(99)
	min, max := 1<<30, -(1 << 30)
	for x := -400; x < 400; x += 7 {
		for z := -400; z < 400; z += 7 {
			h, _ := o.heightAt(x, z)
			min, max = minInt(min, h), maxInt(max, h)
		}
	}
	if min < -60 || max > 300 {
		t.Errorf("height range [%d, %d] escapes the overworld build range", min, max)
	}
	if max-min < 30 {
		t.Errorf("height range [%d, %d] is too flat to be interesting", min, max)
	}
}

// TestDeterministic checks that a seed reproduces a world exactly. Dragonfly
// may call GenerateChunk concurrently, so generation must not depend on
// evaluation order or on shared mutable state.
func TestDeterministic(t *testing.T) {
	for _, k := range []Kind{KindOverworld, KindNether, KindEnd, KindFlat} {
		g1, _ := k.New(7)
		g2, _ := k.New(7)
		c1, c2 := newChunk(k), newChunk(k)
		pos := world.ChunkPos{3, -5}
		g1.GenerateChunk(pos, c1)
		g2.GenerateChunk(pos, c2)

		r := c1.Range()
		for x := range uint8(16) {
			for z := range uint8(16) {
				for y := int16(r.Min()); y <= int16(r.Max()); y++ {
					if a, b := c1.Block(x, y, z, 0), c2.Block(x, y, z, 0); a != b {
						t.Fatalf("%s: block at (%d,%d,%d) differs between runs: %d vs %d", k, x, y, z, a, b)
					}
				}
			}
		}
	}
}

// TestSeedsDiffer guards against a generator that ignores its seed.
func TestSeedsDiffer(t *testing.T) {
	a, _ := KindOverworld.New(1)
	b, _ := KindOverworld.New(2)
	c1, c2 := newChunk(KindOverworld), newChunk(KindOverworld)
	a.GenerateChunk(world.ChunkPos{0, 0}, c1)
	b.GenerateChunk(world.ChunkPos{0, 0}, c2)

	r := c1.Range()
	for x := range uint8(16) {
		for z := range uint8(16) {
			for y := int16(r.Min()); y <= int16(r.Max()); y++ {
				if c1.Block(x, y, z, 0) != c2.Block(x, y, z, 0) {
					return
				}
			}
		}
	}
	t.Error("two different seeds produced an identical chunk")
}

// TestVoidIsEmpty checks the void generator places nothing at all.
func TestVoidIsEmpty(t *testing.T) {
	counts := histogram(t, KindVoid, 5, 0)
	air := world.DefaultBlockRegistry.AirRuntimeID()
	for rid, n := range counts {
		if rid != air && n > 0 {
			t.Errorf("void world contains %d blocks of runtime ID %d", n, rid)
		}
	}
}

// TestNetherAndEnd checks both worlds have solid ground and open space.
func TestNetherAndEnd(t *testing.T) {
	n := NewNether(3)
	counts := histogram(t, KindNether, 3, 1)
	if counts[n.netherrack] < 10000 {
		t.Errorf("nether: only %d netherrack", counts[n.netherrack])
	}
	if counts[n.lava] < 100 {
		t.Errorf("nether: only %d lava", counts[n.lava])
	}
	if counts[n.bedrock] < 100 {
		t.Errorf("nether: only %d bedrock", counts[n.bedrock])
	}

	e := NewEnd(3)
	endCounts := histogram(t, KindEnd, 3, 1)
	if endCounts[e.endStone] < 5000 {
		t.Errorf("end: only %d end stone", endCounts[e.endStone])
	}
	if endCounts[e.obsidian] != 25 {
		t.Errorf("end: spawn platform is %d obsidian, want 25", endCounts[e.obsidian])
	}
}

// TestDefaultSpawnIsOnLand checks players do not spawn inside the ocean.
func TestDefaultSpawnIsOnLand(t *testing.T) {
	for seed := int64(0); seed < 12; seed++ {
		o := NewOverworld(seed)
		pos := o.DefaultSpawn(world.Overworld)
		h, _ := o.heightAt(pos.X(), pos.Z())
		if h <= seaLevel {
			t.Errorf("seed %d: spawn at y=%d sits at or below sea level", seed, h)
		}
		if pos.Y() != h+1 {
			t.Errorf("seed %d: spawn y=%d is not one above the surface %d", seed, pos.Y(), h)
		}
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
