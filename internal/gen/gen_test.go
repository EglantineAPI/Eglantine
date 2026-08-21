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
		{"coal ore", o.ore[oreCoal], 20},
		{"iron ore", o.ore[oreIron], 20},
		{"diamond ore", o.oreDeepslate[oreDiamond], 1},
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

// oreCounts generates a square of chunks and returns the average number of
// blocks of each ore per chunk.
func oreCounts(t *testing.T, seed int64, radius int32) (map[oreKind]float64, int) {
	t.Helper()
	o := NewOverworld(seed)
	byRID := map[uint32]oreKind{}
	for k := oreKind(0); k < oreKindCount; k++ {
		byRID[o.ore[k]] = k
		byRID[o.oreDeepslate[k]] = k
	}

	totals := map[oreKind]int{}
	chunks := 0
	for cx := -radius; cx <= radius; cx++ {
		for cz := -radius; cz <= radius; cz++ {
			c := newChunk(KindOverworld)
			o.GenerateChunk(world.ChunkPos{cx, cz}, c)
			chunks++
			r := c.Range()
			for x := range uint8(16) {
				for z := range uint8(16) {
					for y := int16(r.Min()); y <= int16(r.Max()); y++ {
						if k, ok := byRID[c.Block(x, y, z, 0)]; ok {
							totals[k]++
						}
					}
				}
			}
		}
	}
	avg := map[oreKind]float64{}
	for k, n := range totals {
		avg[k] = float64(n) / float64(chunks)
	}
	return avg, chunks
}

// TestOreDensityIsVanillaScale checks ore is neither absent nor everywhere.
//
// The bounds come from vanilla's own table: 30+20 coal veins of 17 blocks a
// chunk make coal abundant, while diamond gets 14 vein attempts of 4 to 12
// blocks that are then mostly discarded near air, so a chunk holds a handful of
// diamonds rather than a pile.
func TestOreDensityIsVanillaScale(t *testing.T) {
	avg, chunks := oreCounts(t, 4242, 2)
	t.Logf("averages over %d chunks: %v", chunks, avg)

	for _, tc := range []struct {
		kind     oreKind
		name     string
		min, max float64
	}{
		{oreCoal, "coal", 40, 250},
		{oreIron, "iron", 30, 200},
		{oreCopper, "copper", 30, 200},
		{oreGold, "gold", 6, 60},
		{oreRedstone, "redstone", 8, 70},
		{oreLapis, "lapis", 4, 50},
		{oreDiamond, "diamond", 2, 30},
	} {
		got := avg[tc.kind]
		if got < tc.min || got > tc.max {
			t.Errorf("%s: %.1f blocks per chunk, want between %.1f and %.1f", tc.name, got, tc.min, tc.max)
		}
	}
}

// TestOreRarityOrdering checks the ores keep vanilla's relative scarcity.
// A world where diamond is as common as iron is the "roubado" case.
func TestOreRarityOrdering(t *testing.T) {
	avg, _ := oreCounts(t, 4242, 2)
	if avg[oreCoal] <= avg[oreIron] {
		t.Errorf("coal (%.1f) is not more common than iron (%.1f)", avg[oreCoal], avg[oreIron])
	}
	if avg[oreIron] <= avg[oreGold] {
		t.Errorf("iron (%.1f) is not more common than gold (%.1f)", avg[oreIron], avg[oreGold])
	}
	if avg[oreDiamond] >= avg[oreGold] {
		t.Errorf("diamond (%.1f) is not rarer than gold (%.1f)", avg[oreDiamond], avg[oreGold])
	}
	if avg[oreDiamond] >= avg[oreIron] {
		t.Errorf("diamond (%.1f) is not rarer than iron (%.1f)", avg[oreDiamond], avg[oreIron])
	}
	// Diamond has to be the scarcest ore in the world, not merely scarce.
	for k, n := range avg {
		if k != oreDiamond && k != oreEmerald && n <= avg[oreDiamond] {
			t.Errorf("ore %d (%.1f) is not more common than diamond (%.1f)", k, n, avg[oreDiamond])
		}
	}
}

// TestDiamondStaysDeep checks diamond never appears near the surface, which is
// the property that makes finding it a trip to bedrock rather than a stroll.
func TestDiamondStaysDeep(t *testing.T) {
	o := NewOverworld(4242)
	diamond := map[uint32]bool{o.ore[oreDiamond]: true, o.oreDeepslate[oreDiamond]: true}
	for cx := int32(-2); cx <= 2; cx++ {
		for cz := int32(-2); cz <= 2; cz++ {
			c := newChunk(KindOverworld)
			o.GenerateChunk(world.ChunkPos{cx, cz}, c)
			for x := range uint8(16) {
				for z := range uint8(16) {
					for y := int16(17); y <= int16(c.Range().Max()); y++ {
						if diamond[c.Block(x, y, z, 0)] {
							t.Fatalf("diamond at y=%d, above the vanilla ceiling of 16", y)
						}
					}
				}
			}
		}
	}
}

// TestCoalStaysShallow is the mirror check: vanilla coal does not reach the
// deepslate layer, so a deep mine should not be a coal mine.
func TestCoalStaysShallow(t *testing.T) {
	o := NewOverworld(4242)
	coal := map[uint32]bool{o.ore[oreCoal]: true, o.oreDeepslate[oreCoal]: true}
	for cx := int32(-2); cx <= 2; cx++ {
		for cz := int32(-2); cz <= 2; cz++ {
			c := newChunk(KindOverworld)
			o.GenerateChunk(world.ChunkPos{cx, cz}, c)
			for x := range uint8(16) {
				for z := range uint8(16) {
					for y := int16(c.Range().Min()); y < 0; y++ {
						if coal[c.Block(x, y, z, 0)] {
							t.Fatalf("coal at y=%d, below its vanilla floor of 0", y)
						}
					}
				}
			}
		}
	}
}

// TestNoGiantDiamondVeins guards the reported bug of stacks of 30 diamonds.
// Vanilla's largest diamond batch is 12 blocks, so no connected run along any
// axis should approach that.
func TestNoGiantDiamondVeins(t *testing.T) {
	o := NewOverworld(4242)
	diamond := map[uint32]bool{o.ore[oreDiamond]: true, o.oreDeepslate[oreDiamond]: true}

	worst := 0
	for cx := int32(-1); cx <= 1; cx++ {
		for cz := int32(-1); cz <= 1; cz++ {
			c := newChunk(KindOverworld)
			o.GenerateChunk(world.ChunkPos{cx, cz}, c)
			r := c.Range()
			for x := range uint8(16) {
				for z := range uint8(16) {
					run := 0
					for y := int16(r.Min()); y <= int16(r.Max()); y++ {
						if diamond[c.Block(x, y, z, 0)] {
							run++
							worst = max(worst, run)
						} else {
							run = 0
						}
					}
				}
			}
		}
	}
	if worst > 6 {
		t.Errorf("found a vertical run of %d diamond ore; vanilla veins are at most 12 blocks total and never a column that tall", worst)
	}
}

// TestNoTreesOnGravel is the regression for trees growing on river gravel. It
// happened because the decoration pass recomputed the biome with the river
// strength forced to zero, so it disagreed with the surface actually placed.
func TestNoTreesOnGravel(t *testing.T) {
	o := NewOverworld(1234)
	for cx := int32(-3); cx <= 3; cx++ {
		for cz := int32(-3); cz <= 3; cz++ {
			c := newChunk(KindOverworld)
			o.GenerateChunk(world.ChunkPos{cx, cz}, c)
			r := c.Range()
			for x := range uint8(16) {
				for z := range uint8(16) {
					for y := int16(r.Min() + 1); y <= int16(r.Max()); y++ {
						if c.Block(x, y, z, 0) != o.log {
							continue
						}
						below := c.Block(x, y-1, z, 0)
						if below == o.gravel || below == o.sand || below == o.water {
							t.Fatalf("tree trunk at chunk (%d,%d) local (%d,%d,%d) stands on a non-soil block", cx, cz, x, y, z)
						}
					}
				}
			}
		}
	}
}

// TestGravelOnlyUnderwater is the regression for the strips of dry gravel that
// looked like a gravel biome. Gravel is a river or sea bed, so a column whose
// surface is gravel has to be below sea level.
func TestGravelOnlyUnderwater(t *testing.T) {
	o := NewOverworld(1234)
	for x := -900; x < 900; x += 13 {
		for z := -900; z < 900; z += 13 {
			height, river := o.heightAt(x, z)
			b := o.biomeAt(x, z, height, river)
			top, _ := o.surfaceFor(b, height)
			if top == o.gravel && height >= seaLevel {
				t.Fatalf("column (%d,%d) has a gravel surface at y=%d, at or above sea level %d", x, z, height, seaLevel)
			}
		}
	}
}

// TestRiversHoldWater checks a river channel actually reaches below sea level,
// rather than leaving a dry trench where the biome says river.
func TestRiversHoldWater(t *testing.T) {
	o := NewOverworld(1234)
	found, wet := 0, 0
	for x := -2000; x < 2000; x += 3 {
		for z := -2000; z < 2000; z += 61 {
			height, river := o.heightAt(x, z)
			if river < 0.9 {
				continue
			}
			found++
			if height < seaLevel {
				wet++
			}
		}
	}
	if found == 0 {
		t.Skip("no river centres in the sampled area")
	}
	if ratio := float64(wet) / float64(found); ratio < 0.95 {
		t.Errorf("only %.0f%% of river centres are below sea level, want nearly all", ratio*100)
	}
}
