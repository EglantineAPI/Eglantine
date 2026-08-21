package gen

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

// netherCounts generates a square of nether chunks and returns the average
// number of each interesting block per chunk.
func netherCounts(t *testing.T, seed int64, radius int32) map[string]float64 {
	t.Helper()
	n := NewNether(seed)
	names := map[uint32]string{
		n.quartzOre: "quartz", n.goldOre: "gold", n.debris: "debris",
		n.glowstone: "glowstone", n.lava: "lava", n.netherrack: "netherrack",
		n.bedrock: "bedrock", n.magma: "magma", n.blackstone: "blackstone",
	}
	counts, chunks := map[string]int{}, 0
	for cx := -radius; cx <= radius; cx++ {
		for cz := -radius; cz <= radius; cz++ {
			c := newChunk(KindNether)
			n.GenerateChunk(world.ChunkPos{cx, cz}, c)
			chunks++
			r := c.Range()
			for x := range uint8(16) {
				for z := range uint8(16) {
					for y := int16(r.Min()); y <= int16(r.Max()); y++ {
						if name, ok := names[c.Block(x, y, z, 0)]; ok {
							counts[name]++
						}
					}
				}
			}
		}
	}
	avg := map[string]float64{}
	for k, v := range counts {
		avg[k] = float64(v) / float64(chunks)
	}
	return avg
}

// TestNetherHasItsOres is the regression for a nether with no ore in it at all.
func TestNetherHasItsOres(t *testing.T) {
	avg := netherCounts(t, 4242, 3)
	t.Logf("per chunk: %v", avg)

	for _, tc := range []struct {
		name     string
		min, max float64
	}{
		{"quartz", 25, 250},
		{"gold", 8, 120},
		{"glowstone", 4, 60},
		{"netherrack", 5000, 200000},
		{"lava", 500, 200000},
		{"bedrock", 200, 100000},
	} {
		if got := avg[tc.name]; got < tc.min || got > tc.max {
			t.Errorf("%s: %.1f per chunk, want between %.0f and %.0f", tc.name, got, tc.min, tc.max)
		}
	}
}

// TestNetheriteIsRare checks ancient debris exists and stays a real find.
// Netherite is the scarcest thing in the game and should read that way.
func TestNetheriteIsRare(t *testing.T) {
	avg := netherCounts(t, 4242, 3)
	debris := avg["debris"]
	if debris == 0 {
		t.Fatal("no ancient debris anywhere in the sampled area")
	}
	if debris > 3 {
		t.Errorf("ancient debris averages %.2f per chunk, which is not rare", debris)
	}
	// It has to be scarcer than every other nether ore by a wide margin.
	if debris*10 > avg["gold"] {
		t.Errorf("ancient debris (%.2f) is not far rarer than nether gold (%.2f)", debris, avg["gold"])
	}
}

// TestNetheriteStaysDeep checks ancient debris only appears in its vanilla band,
// so it cannot be strip-mined from anywhere in the nether.
func TestNetheriteStaysDeep(t *testing.T) {
	n := NewNether(4242)
	for cx := int32(-3); cx <= 3; cx++ {
		for cz := int32(-3); cz <= 3; cz++ {
			c := newChunk(KindNether)
			n.GenerateChunk(world.ChunkPos{cx, cz}, c)
			for x := range uint8(16) {
				for z := range uint8(16) {
					for y := int16(c.Range().Min()); y <= int16(c.Range().Max()); y++ {
						if c.Block(x, y, z, 0) != n.debris {
							continue
						}
						if y < 5 || y > 122 {
							t.Fatalf("ancient debris at y=%d, outside its band", y)
						}
					}
				}
			}
		}
	}
}

// TestGlowstoneIsClustered is the complaint that glowstone turned up as lone
// blocks. It is a cluster feature: where it appears, it appears in quantity.
func TestGlowstoneIsClustered(t *testing.T) {
	n := NewNether(4242)
	biggest, chunksWith, chunks := 0, 0, 0

	for cx := int32(-3); cx <= 3; cx++ {
		for cz := int32(-3); cz <= 3; cz++ {
			c := newChunk(KindNether)
			n.GenerateChunk(world.ChunkPos{cx, cz}, c)
			chunks++
			found := 0
			for x := range uint8(16) {
				for z := range uint8(16) {
					for y := int16(c.Range().Min()); y <= int16(c.Range().Max()); y++ {
						if c.Block(x, y, z, 0) == n.glowstone {
							found++
						}
					}
				}
			}
			if found > 0 {
				chunksWith++
				biggest = max(biggest, found)
			}
		}
	}
	if biggest < 10 {
		t.Errorf("the largest glowstone group is %d blocks; it should be a cluster", biggest)
	}
	// Not every chunk should have one, or it stops being a landmark.
	if share := float64(chunksWith) / float64(chunks); share > 0.9 {
		t.Errorf("%.0f%% of chunks have glowstone; it should be a find", share*100)
	}
}

// TestNetherIsClosed checks the bedrock shell, so players cannot fall out of
// the world or walk onto the roof.
func TestNetherIsClosed(t *testing.T) {
	n := NewNether(7)
	c := newChunk(KindNether)
	n.GenerateChunk(world.ChunkPos{0, 0}, c)
	minY := int16(c.Range().Min())

	for x := range uint8(16) {
		for z := range uint8(16) {
			if got := c.Block(x, minY, z, 0); got != n.bedrock {
				t.Fatalf("the floor at (%d,%d) is %d, not bedrock", x, z, got)
			}
			if got := c.Block(x, netherRoof, z, 0); got != n.bedrock {
				t.Fatalf("the ceiling at (%d,%d) is %d, not bedrock", x, z, got)
			}
		}
	}
}
