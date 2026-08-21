package gen

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// endCentreY is the y the main island is built around.
const endCentreY = 64

// End generates the end: a solid central island around the origin, ringed by
// empty space, with smaller islands scattered further out.
type End struct {
	shape *perlin
	seed  int64

	endStone, obsidian uint32
}

// NewEnd builds an End generator for the seed passed.
func NewEnd(seed int64) *End {
	br := world.DefaultBlockRegistry
	rid := func(b world.Block) uint32 { return br.BlockRuntimeID(b) }
	return &End{
		seed:     seed,
		shape:    newPerlin(seed + 21),
		endStone: rid(block.EndStone{}),
		obsidian: rid(block.Obsidian{}),
	}
}

// density returns how solid a column is. Above zero means island.
func (e *End) density(x, z int) float64 {
	d := distance(x, z)
	n := e.shape.fbm(float64(x), float64(z), 3, 1.0/120.0, 0.5)

	switch {
	case d < 90:
		// Main island: solid in the middle, tapering to a ragged rim.
		return 1 - d/90 + n*0.25
	case d < 190:
		// The void ring that separates the main island from the outer ones.
		return -1
	default:
		// Outer islands: only the peaks of the noise field rise into being.
		return n - 0.42
	}
}

func distance(x, z int) float64 {
	fx, fz := float64(x), float64(z)
	return sqrt(fx*fx + fz*fz)
}

// sqrt is Newton's method, avoiding a math import for one call site.
func sqrt(v float64) float64 {
	if v <= 0 {
		return 0
	}
	g := v
	for range 20 {
		g = 0.5 * (g + v/g)
	}
	return g
}

// GenerateChunk implements world.Generator.
func (e *End) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	min, max := int16(c.Range().Min()), int16(c.Range().Max())
	baseX, baseZ := int(pos.X())*16, int(pos.Z())*16
	bid := uint32(biome.End{}.EncodeBiome())

	for lx := range 16 {
		for lz := range 16 {
			wx, wz := baseX+lx, baseZ+lz
			x, z := uint8(lx), uint8(lz)

			d := e.density(wx, wz)
			// Thickness follows density, so islands are domed rather than slabs.
			half := int(d * 14)

			for y := min; y <= max; y++ {
				c.SetBiome(x, y, z, bid)
				if d <= 0 || half <= 0 {
					continue
				}
				wy := int(y)
				if wy >= endCentreY-half && wy <= endCentreY+half/2 {
					c.SetBlock(x, y, z, 0, e.endStone)
				}
			}
		}
	}
	e.placeSpawnPlatform(pos, c)
}

// placeSpawnPlatform lays the obsidian pad at the origin, so arriving players
// always have solid ground under them.
func (e *End) placeSpawnPlatform(pos world.ChunkPos, c *chunk.Chunk) {
	if pos.X() != 0 || pos.Z() != 0 {
		return
	}
	for lx := range 5 {
		for lz := range 5 {
			c.SetBlock(uint8(lx), int16(endCentreY+16), uint8(lz), 0, e.obsidian)
		}
	}
}

// DefaultSpawn implements world.Generator.
func (e *End) DefaultSpawn(world.Dimension) cube.Pos {
	return cube.Pos{2, endCentreY + 17, 2}
}
