package gen

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

// biomeMix samples a wide area and returns the share of each biome.
func biomeMix(seed int64) map[biomeKind]float64 {
	o := NewOverworld(seed)
	counts, total := map[biomeKind]int{}, 0
	for x := -2600; x < 2600; x += 13 {
		for z := -2600; z < 2600; z += 13 {
			h, r := o.heightAt(x, z)
			counts[o.biomeAt(x, z, h, r)]++
			total++
		}
	}
	mix := map[biomeKind]float64{}
	for k, n := range counts {
		mix[k] = float64(n) / float64(total)
	}
	return mix
}

// TestBiomeMix guards the two complaints about the old selection — the world
// was mostly beach and bare stone peaks — and the two things it was missing,
// plains and any real variety.
func TestBiomeMix(t *testing.T) {
	mix := biomeMix(1234)

	if beach := mix[bBeach]; beach > 0.03 {
		t.Errorf("beach covers %.1f%% of the world, want under 3%%", beach*100)
	}
	if peaks := mix[bStonyPeaks] + mix[bFrozenPeaks]; peaks > 0.03 {
		t.Errorf("bare peaks cover %.1f%%, want under 3%%", peaks*100)
	}
	if plains := mix[bPlains] + mix[bSunflowerPlains]; plains < 0.08 {
		t.Errorf("plains cover only %.1f%%, want at least 8%%", plains*100)
	}
	if ocean := mix[bOcean] + mix[bDeepOcean]; ocean < 0.20 || ocean > 0.50 {
		t.Errorf("ocean covers %.1f%%, want between 20%% and 50%%", ocean*100)
	}

	// Every biome in the table has to be reachable. One that never appears is
	// dead code that looks like content.
	for k := biomeKind(0); k < biomeKindCount; k++ {
		if mix[k] == 0 {
			t.Errorf("%s never appears", biomeTable[k].biome.String())
		}
	}
	// The biomes asked for by name should be findable, not merely present.
	for _, tc := range []struct {
		kind biomeKind
		min  float64
	}{
		{bDesert, 0.005}, {bSwamp, 0.003}, {bJungle, 0.005},
		{bBirchForest, 0.01}, {bDarkForest, 0.003}, {bTaiga, 0.02}, {bMeadow, 0.005},
	} {
		if mix[tc.kind] < tc.min {
			t.Errorf("%s covers %.2f%%, want at least %.2f%%",
				biomeTable[tc.kind].biome.String(), mix[tc.kind]*100, tc.min*100)
		}
	}
}

// scan generates a square of chunks and calls f for every block.
func scan(t *testing.T, o *Overworld, radius int32, f func(c chunkView, x uint8, y int16, z uint8, rid uint32)) {
	t.Helper()
	for cx := -radius; cx <= radius; cx++ {
		for cz := -radius; cz <= radius; cz++ {
			c := newChunk(KindOverworld)
			o.GenerateChunk(world.ChunkPos{cx, cz}, c)
			r := c.Range()
			view := chunkView{c: c, min: int16(r.Min()), max: int16(r.Max())}
			for x := range uint8(16) {
				for z := range uint8(16) {
					for y := int16(r.Min()); y <= int16(r.Max()); y++ {
						f(view, x, y, z, c.Block(x, y, z, 0))
					}
				}
			}
		}
	}
}

// chunkView reads blocks from a chunk. Reading outside the build range returns
// zero rather than panicking, so callers can look at the block below the very
// bottom of the world without a bounds check of their own.
type chunkView struct {
	c interface {
		Block(uint8, int16, uint8, uint8) uint32
	}
	min, max int16
}

func (v chunkView) at(x uint8, y int16, z uint8) uint32 {
	if y < v.min || y > v.max {
		return 0
	}
	return v.c.Block(x, y, z, 0)
}

// TestTreesVaryByBiome checks the world grows more than oak. A single wood type
// everywhere is what made every forest look the same.
func TestTreesVaryByBiome(t *testing.T) {
	o := NewOverworld(1234)
	seen := map[treeKind]int{}
	byRID := map[uint32]treeKind{}
	for k := treeKind(1); k < treeKindCount; k++ {
		byRID[o.treeLog[k]] = k
	}
	// Birch, dark forest and jungle are each a few percent of the world, so a
	// small square around the origin can miss them entirely. Sampling chunks
	// spread across a wide area finds them without generating the whole map.
	for cx := int32(-30); cx <= 30; cx += 5 {
		for cz := int32(-30); cz <= 30; cz += 5 {
			c := newChunk(KindOverworld)
			o.GenerateChunk(world.ChunkPos{cx, cz}, c)
			r := c.Range()
			for x := range uint8(16) {
				for z := range uint8(16) {
					for y := int16(seaLevel); y <= int16(r.Max()); y++ {
						if k, ok := byRID[c.Block(x, y, z, 0)]; ok {
							seen[k]++
						}
					}
				}
			}
		}
	}
	// Oak and swamp oak share a log, so they cannot be told apart here and
	// count as one.
	distinct := 0
	for k := range seen {
		if seen[k] > 0 {
			distinct++
		}
	}
	if distinct < 4 {
		t.Errorf("only %d distinct wood types grow: %v", distinct, seen)
	}
	for _, tc := range []struct {
		kind treeKind
		name string
	}{
		{treeBirch, "birch"}, {treeSpruce, "spruce"},
		{treeAcacia, "acacia"}, {treeDarkOak, "dark oak"}, {treeJungle, "jungle"},
	} {
		if seen[tc.kind] == 0 {
			t.Errorf("no %s trees found", tc.name)
		}
	}
}

// TestUnderwaterVegetation checks the sea is not bare, which was the complaint
// about rivers and oceans having nothing growing in them.
func TestUnderwaterVegetation(t *testing.T) {
	o := NewOverworld(1234)
	kelp := 0
	scan(t, o, 5, func(_ chunkView, _ uint8, _ int16, _ uint8, rid uint32) {
		if rid == o.plants.kelp {
			kelp++
		}
	})
	if kelp < 50 {
		t.Errorf("only %d kelp blocks in 121 chunks; the water is bare", kelp)
	}
}

// TestPlantsStandOnSomething checks nothing floats. A plant with air under it
// pops the moment the chunk is ticked, which looks like the world is decaying.
func TestPlantsStandOnSomething(t *testing.T) {
	o := NewOverworld(99)
	p := &o.plants
	ground := map[uint32]bool{
		o.grass: true, o.dirt: true, o.sand: true, o.podzol: true,
		o.snow: true, o.stone: true, o.deepslate: true, o.gravel: true,
		o.clay: true,
	}
	scan(t, o, 3, func(v chunkView, x uint8, y int16, z uint8, rid uint32) {
		var ok bool
		below := v.at(x, y-1, z)
		switch rid {
		case p.shortGrass, p.fern, p.deadBush, p.tallGrassLower, p.mossCarpet:
			ok = ground[below]
		case p.tallGrassUpper:
			ok = below == p.tallGrassLower
		case p.cactus:
			ok = below == o.sand || below == p.cactus
		case p.kelp:
			ok = below == o.water || below == p.kelp || ground[below]
		case p.lilyPad:
			ok = below == o.water
		default:
			for _, f := range p.flower {
				if rid == f {
					ok = ground[below]
					break
				}
			}
			return
		}
		if !ok {
			t.Fatalf("plant %d at y=%d has %d under it", rid, y, below)
		}
	})
}

// TestCaveShape checks the caves match what was asked for: fewer winding
// tunnels near the surface, and large open chambers down in the deepslate.
func TestCaveShape(t *testing.T) {
	o := NewOverworld(4242)
	// The cave field is built per chunk, so the sample walks chunk by chunk.
	measure := func(lo, hi int) float64 {
		total, carved := 0, 0
		for cx := range 12 {
			for cz := range 12 {
				f := o.newCaveField(chunkPos{x: cx, z: cz})
				var col caveColumn
				for lx := 0; lx < 16; lx += 2 {
					for lz := 0; lz < 16; lz += 2 {
						x, z := cx*16+lx, cz*16+lz
						h, _ := o.heightAt(x, z)
						cc := o.columnCaveAt(x, z, h)
						f.column(x, z, &col)
						for y := lo; y < hi; y++ {
							if y > h {
								continue
							}
							total++
							if o.carvedAt(&col, x, y, z, h, cc) {
								carved++
							}
						}
					}
				}
			}
		}
		if total == 0 {
			return 0
		}
		return float64(carved) / float64(total)
	}

	shallow, deep := measure(10, 55), measure(-59, -20)
	if shallow > 0.12 {
		t.Errorf("%.1f%% of the shallow rock is hollow, want under 12%%", shallow*100)
	}
	if deep < 0.15 {
		t.Errorf("only %.1f%% of the deep rock is hollow, want at least 15%%", deep*100)
	}
	if deep <= shallow {
		t.Errorf("deep rock (%.1f%%) is not more open than shallow rock (%.1f%%)", deep*100, shallow*100)
	}
}

// TestRavinesExist checks ravines are generated at all, and stay uncommon.
func TestRavinesExist(t *testing.T) {
	o := NewOverworld(1234)
	inRavine, total := 0, 0
	for x := -1500; x < 1500; x += 5 {
		for z := -1500; z < 1500; z += 37 {
			h, _ := o.heightAt(x, z)
			total++
			if o.columnCaveAt(x, z, h).ravine {
				inRavine++
			}
		}
	}
	share := float64(inRavine) / float64(total)
	if inRavine == 0 {
		t.Fatal("no ravines anywhere in the sampled area")
	}
	if share > 0.03 {
		t.Errorf("ravines cover %.2f%% of columns, want under 3%%", share*100)
	}
}

// TestLushCavesAreRare checks the overgrown pockets exist but stay a find.
func TestLushCavesAreRare(t *testing.T) {
	o := NewOverworld(1234)
	lush, total := 0, 0
	for x := 0; x < 400; x += 3 {
		for z := 0; z < 400; z += 3 {
			for y := -50; y < 20; y += 5 {
				total++
				if o.lushAt(x, y, z) {
					lush++
				}
			}
		}
	}
	share := float64(lush) / float64(total)
	if lush == 0 {
		t.Fatal("no lush cave pockets anywhere in the sampled area")
	}
	if share > 0.05 {
		t.Errorf("lush pockets fill %.2f%% of the underground, want under 5%%", share*100)
	}
}

// TestSurfaceOpeningsExist checks some caves break the surface, so a player can
// walk into one instead of always having to dig.
func TestSurfaceOpeningsExist(t *testing.T) {
	o := NewOverworld(1234)
	open, total := 0, 0
	for x := -800; x < 800; x += 7 {
		for z := -800; z < 800; z += 7 {
			h, _ := o.heightAt(x, z)
			total++
			if o.columnCaveAt(x, z, h).openSurface {
				open++
			}
		}
	}
	share := float64(open) / float64(total)
	if share < 0.01 {
		t.Errorf("only %.2f%% of columns allow a cave mouth, want at least 1%%", share*100)
	}
	if share > 0.35 {
		t.Errorf("%.1f%% of columns allow a cave mouth; the surface would be full of holes", share*100)
	}
}

// TestKelpIsWaterlogged is the regression for kelp breaking the moment a chunk
// was ticked. A chunk holds the block and the liquid around it on two separate
// layers; writing only the first removed the water the kelp was standing in.
func TestKelpIsWaterlogged(t *testing.T) {
	o := NewOverworld(1234)
	found := 0
	for cx := int32(-4); cx <= 4; cx++ {
		for cz := int32(-4); cz <= 4; cz++ {
			c := newChunk(KindOverworld)
			o.GenerateChunk(world.ChunkPos{cx, cz}, c)
			for x := range uint8(16) {
				for z := range uint8(16) {
					for y := int16(c.Range().Min()); y <= int16(seaLevel); y++ {
						if c.Block(x, y, z, 0) != o.plants.kelp {
							continue
						}
						found++
						if liquid := c.Block(x, y, z, 1); liquid != o.water {
							t.Fatalf("kelp at y=%d has %d on the liquid layer, not water", y, liquid)
						}
					}
				}
			}
		}
	}
	if found == 0 {
		t.Fatal("no kelp anywhere in the sampled area")
	}
}

// TestSeabedVaries checks the sea floor is not one flat sheet of gravel.
func TestSeabedVaries(t *testing.T) {
	o := NewOverworld(1234)
	seen := map[uint32]int{}
	for x := -900; x < 900; x += 7 {
		for z := -900; z < 900; z += 7 {
			height, _ := o.heightAt(x, z)
			if height >= seaLevel {
				continue
			}
			top, _ := o.seabedAt(x, z, seaLevel-height)
			seen[top]++
		}
	}
	if len(seen) < 3 {
		t.Errorf("the sea floor uses only %d materials, want at least 3", len(seen))
	}
	for _, tc := range []struct {
		rid  uint32
		name string
	}{{o.sand, "sand"}, {o.gravel, "gravel"}, {o.dirt, "dirt"}} {
		if seen[tc.rid] == 0 {
			t.Errorf("the sea floor never uses %s", tc.name)
		}
	}
}

// TestLavaLakesReachTheSurface checks surface lava pools generate, and that
// they are rimmed rather than lava simply meeting grass.
func TestLavaLakesReachTheSurface(t *testing.T) {
	o := NewOverworld(1234)
	lakes, rimmed := 0, 0

	for cx := int32(-8); cx <= 8; cx++ {
		for cz := int32(-8); cz <= 8; cz++ {
			c := newChunk(KindOverworld)
			o.GenerateChunk(world.ChunkPos{cx, cz}, c)
			for x := uint8(1); x < 15; x++ {
				for z := uint8(1); z < 15; z++ {
					for y := int16(seaLevel + 4); y <= int16(200); y++ {
						if c.Block(x, y, z, 0) != o.lava {
							continue
						}
						lakes++
						// Somewhere around a surface pool there has to be the
						// stone rim the feature lays down.
						for _, d := range [4][2]int8{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
							if c.Block(uint8(int8(x)+d[0]), y, uint8(int8(z)+d[1]), 0) == o.stone {
								rimmed++
								break
							}
						}
						break
					}
				}
			}
		}
	}
	if lakes == 0 {
		t.Fatal("no surface lava anywhere in 289 chunks")
	}
	if rimmed == 0 {
		t.Error("surface lava has no stone rim around it")
	}
}

// TestColdCoastsAreSnowy is the complaint about small sand beaches next to ice
// fields. A cold shore should read as cold.
func TestColdCoastsAreSnowy(t *testing.T) {
	o := NewOverworld(1234)
	sandy, snowy := 0, 0
	for x := -2000; x < 2000; x += 11 {
		for z := -2000; z < 2000; z += 11 {
			height, river := o.heightAt(x, z)
			if height < seaLevel || height > beachTop {
				continue
			}
			switch o.biomeAt(x, z, height, river) {
			case bBeach:
				sandy++
			case bSnowyBeach:
				snowy++
			}
		}
	}
	if snowy == 0 {
		t.Fatal("no snowy beaches anywhere; every cold coast is sand")
	}
	if sandy == 0 {
		t.Error("no ordinary beaches at all")
	}
}

// TestColdWaterFreezes is the regression for snowfields running straight into
// open water. Ice has no Go type in Dragonfly but is a registered block state,
// so it resolves by name.
func TestColdWaterFreezes(t *testing.T) {
	o := NewOverworld(1234)
	if o.ice == o.water || o.ice == 0 {
		t.Fatalf("ice did not resolve; got runtime ID %d", o.ice)
	}

	frozenColumns, iceBlocks := 0, 0
	for cx := int32(-6); cx <= 6; cx++ {
		for cz := int32(-6); cz <= 6; cz++ {
			c := newChunk(KindOverworld)
			o.GenerateChunk(world.ChunkPos{cx, cz}, c)
			for x := range uint8(16) {
				for z := range uint8(16) {
					wx, wz := int(cx)*16+int(x), int(cz)*16+int(z)
					h, river := o.heightAt(wx, wz)
					kind := o.biomeAt(wx, wz, h, river)
					if kind != bFrozenOcean && kind != bFrozenRiver {
						continue
					}
					frozenColumns++
					top := c.Block(x, int16(seaLevel), z, 0)
					if top != o.ice {
						t.Fatalf("frozen biome column at (%d,%d) has %d on the surface, not ice", wx, wz, top)
					}
					iceBlocks++
					// There has to be water under the sheet, not a void.
					if below := c.Block(x, int16(seaLevel-1), z, 0); below != o.water && below == o.air {
						t.Fatalf("nothing under the ice at (%d,%d)", wx, wz)
					}
				}
			}
		}
	}
	if frozenColumns == 0 {
		t.Skip("no frozen water in the sampled area")
	}
	t.Logf("%d frozen columns, all capped with ice", iceBlocks)
}

// TestAzaleaGrowsInLushCaves checks the lush pockets got their bush.
func TestAzaleaGrowsInLushCaves(t *testing.T) {
	o := NewOverworld(1234)
	if o.azalea == 0 {
		t.Fatal("azalea did not resolve")
	}
	found := 0
	for cx := int32(-6); cx <= 6; cx++ {
		for cz := int32(-6); cz <= 6; cz++ {
			c := newChunk(KindOverworld)
			o.GenerateChunk(world.ChunkPos{cx, cz}, c)
			for x := range uint8(16) {
				for z := range uint8(16) {
					for y := int16(c.Range().Min()); y <= int16(24); y++ {
						if c.Block(x, y, z, 0) == o.azalea {
							found++
						}
					}
				}
			}
		}
	}
	if found == 0 {
		t.Error("no azalea anywhere in 169 chunks of lush caves")
	}
}
