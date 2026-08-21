package gen

import "github.com/df-mc/dragonfly/server/world/chunk"

// lavaLakeChance is the per-chunk probability of a surface lava lake, scaled by
// 1e-4. Lava on the surface is a hazard and a landmark, so it wants to be
// something you come across rather than something you see from every hilltop.
const lavaLakeChance = 700

// growLavaLake places a small lava pool set into the ground, lined with stone.
//
// The whole feature is kept inside one chunk. A lake straddling a chunk border
// would have to write into its neighbour, which GenerateChunk cannot do, and
// half a lake is worse than none.
func (o *Overworld) growLavaLake(c *chunk.Chunk, pos chunkPos, cols *columns) {

	r := &detRand{s: hashMix(pos.x, 3, pos.z, uint64(o.seed)+0x1a7a)}
	if r.intn(10000) >= lavaLakeChance {
		return
	}

	// Radius three plus a one-block rim, so the centre stays four blocks clear
	// of every edge.
	const radius = 3
	lx, lz := 4+r.intn(8), 4+r.intn(8)
	height := cols.height[lx][lz]
	kind := cols.biome[lx][lz]

	// Lakes sit on dry, reasonably flat ground. Anywhere near the waterline the
	// pool would simply drain into the sea.
	if height <= seaLevel+3 || kind == bOcean || kind == bDeepOcean || kind == bRiver || kind == bBeach {
		return
	}
	for dx := -radius - 1; dx <= radius+1; dx++ {
		for dz := -radius - 1; dz <= radius+1; dz++ {
			if h := cols.height[lx+dx][lz+dz]; h < height-2 || h > height+2 {
				return
			}
		}
	}

	surface := height
	for dx := -radius - 1; dx <= radius+1; dx++ {
		for dz := -radius - 1; dz <= radius+1; dz++ {
			dist := dx*dx + dz*dz
			x, z := lx+dx, lz+dz

			switch {
			case dist <= radius*radius-2:
				// The pool itself: two blocks of lava sunk into the ground,
				// with the surface of the lava level with the surrounding land.
				o.clearAbove(c, x, surface, z)
				setIfInside(c, x, surface, z, o.lava)
				setIfInside(c, x, surface-1, z, o.lava)
				setIfInside(c, x, surface-2, z, o.stone)
			case dist <= (radius+1)*(radius+1):
				// The rim. Stone rather than the biome's own surface, so the
				// lake reads as burnt ground rather than grass meeting lava.
				o.clearAbove(c, x, surface, z)
				setIfInside(c, x, surface, z, o.stone)
				setIfInside(c, x, surface-1, z, o.stone)
			}
		}
	}
}

// clearAbove removes whatever the decoration pass had already grown on a
// column, so a lake does not leave grass and flowers floating over lava.
func (o *Overworld) clearAbove(c *chunk.Chunk, lx, surface, lz int) {
	if lx < 0 || lx > 15 || lz < 0 || lz > 15 {
		return
	}
	maxY := int(c.Range().Max())
	for y := surface + 1; y <= minInt(surface+3, maxY); y++ {
		c.SetBlock(uint8(lx), int16(y), uint8(lz), 0, o.air)
	}
}
