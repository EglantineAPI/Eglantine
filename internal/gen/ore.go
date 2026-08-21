package gen

import (
	"math"

	"github.com/df-mc/dragonfly/server/world/chunk"
)

// This file reproduces vanilla's ore placement. The numbers come from the
// game's own worldgen data: the attempt counts and height ranges from
// data/minecraft/worldgen/placed_feature/ore_*.json, and the vein sizes and
// air-exposure discard chances from data/minecraft/worldgen/feature/ore_*.json.
//
// Vanilla places ore per chunk, not per block: for each batch it makes `count`
// attempts, each dropping one vein of up to `size` blocks at a height drawn
// from the batch's distribution. Deciding ore per block instead — rolling a
// probability at every stone position — is what produces a world where ore is
// everywhere and rare ore is not rare.

// oreKind indexes the runtime ID tables on Overworld.
type oreKind int

const (
	oreCoal oreKind = iota
	oreIron
	oreCopper
	oreGold
	oreRedstone
	oreDiamond
	oreLapis
	oreEmerald
	oreKindCount
)

// heightDist is how a batch draws the y of a vein.
type heightDist int

const (
	// distUniform spreads veins evenly across the range.
	distUniform heightDist = iota
	// distTriangle peaks in the middle of the range and falls off linearly to
	// both edges. Vanilla calls this a trapezoid with no plateau.
	distTriangle
)

// biomeLimit restricts a batch to part of the world.
type biomeLimit int

const (
	// limitNone lets the batch generate anywhere.
	limitNone biomeLimit = iota
	// limitMountain restricts the batch to peak biomes, as vanilla does for
	// emerald.
	limitMountain
)

// oreBatch is one entry of vanilla's ore table.
type oreBatch struct {
	kind oreKind
	// count is the number of veins attempted per chunk.
	count int
	// size is the number of blocks a vein tries to place.
	size int
	dist heightDist
	// minY and maxY bound the vein centre, inclusive.
	minY, maxY int
	// discardOnAir is the chance of skipping a block that touches air. It is
	// what keeps buried ore out of cave walls.
	discardOnAir float64
	limit        biomeLimit
}

// oreBatches is vanilla's overworld ore table.
//
// Two batches are deliberately absent: copper_large and gold_extra apply only
// to badlands, a biome this generator does not produce.
var oreBatches = []oreBatch{
	// Coal: very common, and the upper batch is why surface coal is easy.
	{kind: oreCoal, count: 30, size: 17, dist: distUniform, minY: 136, maxY: 320},
	{kind: oreCoal, count: 20, size: 17, dist: distTriangle, minY: 0, maxY: 192, discardOnAir: 0.5},

	// Iron: three batches. The 90-attempt upper one covers mountains, where
	// most of it lands above the terrain and places nothing.
	{kind: oreIron, count: 90, size: 9, dist: distTriangle, minY: 80, maxY: 384},
	{kind: oreIron, count: 10, size: 9, dist: distTriangle, minY: -24, maxY: 56},
	{kind: oreIron, count: 10, size: 4, dist: distUniform, minY: -64, maxY: 72},

	{kind: oreCopper, count: 16, size: 10, dist: distTriangle, minY: -16, maxY: 112},

	{kind: oreGold, count: 4, size: 9, dist: distTriangle, minY: -64, maxY: 32, discardOnAir: 0.5},
	{kind: oreGold, count: 1, size: 9, dist: distUniform, minY: -64, maxY: -48, discardOnAir: 0.5},

	{kind: oreRedstone, count: 4, size: 8, dist: distUniform, minY: -64, maxY: 15},
	// above_bottom -32..32 around a bottom of -64.
	{kind: oreRedstone, count: 8, size: 8, dist: distTriangle, minY: -96, maxY: -32},

	// Diamond: four batches, small and heavily discarded near air. Three of
	// them are ranged above_bottom -80..80, which is -144..16 for a world whose
	// bottom is -64. Half of that distribution falls below the world and is
	// thrown away, and that is most of why diamond is rare and why what
	// survives clusters just above bedrock. Clamping the range to the buildable
	// area instead would compress the whole distribution into the world and
	// multiply the yield several times over.
	{kind: oreDiamond, count: 7, size: 4, dist: distTriangle, minY: -144, maxY: 16, discardOnAir: 0.5},
	{kind: oreDiamond, count: 2, size: 8, dist: distUniform, minY: -64, maxY: -4, discardOnAir: 0.5},
	{kind: oreDiamond, count: 1, size: 12, dist: distTriangle, minY: -144, maxY: 16, discardOnAir: 0.7},
	{kind: oreDiamond, count: 4, size: 8, dist: distTriangle, minY: -144, maxY: 16, discardOnAir: 1.0},

	{kind: oreLapis, count: 2, size: 7, dist: distTriangle, minY: -32, maxY: 32},
	{kind: oreLapis, count: 4, size: 7, dist: distUniform, minY: -64, maxY: 64, discardOnAir: 1.0},

	{kind: oreEmerald, count: 100, size: 3, dist: distTriangle, minY: -16, maxY: 480, limit: limitMountain},
}

// maxVeinReach bounds how far a vein can extend from its centre. It decides how
// many neighbouring chunks have to be considered so veins are not clipped at
// chunk borders.
const maxVeinReach = 4

// detRand is a deterministic splitmix64 stream. Ore placement must depend only
// on chunk position and seed: Dragonfly generates chunks concurrently, and a
// chunk is generated again from scratch whenever it is not on disk.
type detRand struct{ s uint64 }

func (r *detRand) next() uint64 {
	r.s += 0x9e3779b97f4a7c15
	z := r.s
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// intn returns a value in [0, n).
func (r *detRand) intn(n int) int {
	if n <= 0 {
		return 0
	}
	return int(r.next() % uint64(n))
}

// float returns a value in [0, 1).
func (r *detRand) float() float64 {
	return float64(r.next()>>11) / float64(1<<53)
}

// pick draws a vein centre height from the batch's distribution.
func (b oreBatch) pick(r *detRand) int {
	span := b.maxY - b.minY + 1
	if span <= 1 {
		return b.minY
	}
	if b.dist == distUniform {
		return b.minY + r.intn(span)
	}
	// Averaging two uniform draws gives the triangular distribution vanilla
	// produces from a trapezoid with no plateau.
	return b.minY + (r.intn(span)+r.intn(span))/2
}

// placeOres runs every batch for the chunk at pos, including veins originating
// in the eight neighbouring chunks whose blobs reach across the border.
func (o *Overworld) placeOres(pos chunkPos, c *chunk.Chunk, cols *columns) {
	minY, maxY := int(c.Range().Min()), int(c.Range().Max())

	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			o.placeChunkOres(pos.x+dx, pos.z+dz, pos, c, cols, minY, maxY)
		}
	}
}

// placeChunkOres places the veins belonging to chunk (ox, oz), writing only the
// blocks that land inside the chunk at pos.
func (o *Overworld) placeChunkOres(ox, oz int, pos chunkPos, c *chunk.Chunk, cols *columns, minY, maxY int) {
	baseX, baseZ := ox*16, oz*16
	// The chunk being written, in world coordinates.
	wx0, wz0 := pos.x*16, pos.z*16

	for bi, batch := range oreBatches {
		if batch.limit == limitMountain && !cols.hasMountain {
			// Emerald makes a hundred attempts per chunk; skipping the whole
			// batch where no peak biome exists saves the bulk of that work.
			continue
		}
		r := &detRand{s: hashMix(ox, bi, oz, uint64(o.seed)+0x0e5a)}
		for attempt := 0; attempt < batch.count; attempt++ {
			cx := baseX + r.intn(16)
			cz := baseZ + r.intn(16)
			cy := batch.pick(r)

			// Reject veins that cannot reach this chunk before doing the work
			// of shaping one.
			if cx+maxVeinReach < wx0 || cx-maxVeinReach >= wx0+16 ||
				cz+maxVeinReach < wz0 || cz-maxVeinReach >= wz0+16 ||
				cy+maxVeinReach < minY || cy-maxVeinReach > maxY {
				// The stream still has to advance, or rejecting a vein would
				// shift every later vein of this batch.
				r.next()
				continue
			}
			o.placeVein(batch, r, c, cols, cx, cy, cz, wx0, wz0, minY, maxY)
		}
	}
}

// placeVein writes one vein, following the shape vanilla's ore feature makes.
//
// Vanilla does not place `size` blocks. It stretches a short segment through
// the vein centre, gives each step along it a radius that tapers to nothing at
// both ends, and fills the ellipsoid around that segment. The number of blocks
// that actually land is well under `size` for small veins, which is a large
// part of why a diamond find is a few blocks rather than a pile.
func (o *Overworld) placeVein(batch oreBatch, r *detRand, c *chunk.Chunk, cols *columns, cx, cy, cz, wx0, wz0, minY, maxY int) {
	vr := &detRand{s: r.next()}

	angle := vr.float() * math.Pi
	spread := float64(batch.size) / 8

	// The segment the vein runs along, and the two heights it runs between.
	x0 := float64(cx) + math.Sin(angle)*spread
	x1 := float64(cx) - math.Sin(angle)*spread
	z0 := float64(cz) + math.Cos(angle)*spread
	z1 := float64(cz) - math.Cos(angle)*spread
	y0 := float64(cy + vr.intn(3) - 2)
	y1 := float64(cy + vr.intn(3) - 2)

	rid, deepRID := o.ore[batch.kind], o.oreDeepslate[batch.kind]

	for k := 0; k < batch.size; k++ {
		t := float64(k) / float64(batch.size)
		px := x0 + (x1-x0)*t
		py := y0 + (y1-y0)*t
		pz := z0 + (z1-z0)*t

		// Radius tapers with a sine so the vein is thickest in the middle.
		scale := vr.float() * float64(batch.size) / 16
		radius := ((math.Sin(math.Pi*t)+1)*scale + 1) / 2

		o.fillBlob(batch, vr, c, px, py, pz, radius, rid, deepRID, wx0, wz0, minY, maxY)
	}
}

// fillBlob places ore in the sphere of the radius passed around a point.
func (o *Overworld) fillBlob(batch oreBatch, vr *detRand, c *chunk.Chunk, px, py, pz, radius float64, rid, deepRID uint32, wx0, wz0, minY, maxY int) {
	xLo, xHi := int(math.Floor(px-radius)), int(math.Floor(px+radius))
	yLo, yHi := int(math.Floor(py-radius)), int(math.Floor(py+radius))
	zLo, zHi := int(math.Floor(pz-radius)), int(math.Floor(pz+radius))

	for bx := xLo; bx <= xHi; bx++ {
		dx := (float64(bx) + 0.5 - px) / radius
		if dx*dx >= 1 {
			continue
		}
		for by := yLo; by <= yHi; by++ {
			dy := (float64(by) + 0.5 - py) / radius
			if dx*dx+dy*dy >= 1 || by < minY || by > maxY {
				continue
			}
			for bz := zLo; bz <= zHi; bz++ {
				dz := (float64(bz) + 0.5 - pz) / radius
				if dx*dx+dy*dy+dz*dz >= 1 {
					continue
				}
				lx, lz := bx-wx0, bz-wz0
				if lx < 0 || lx > 15 || lz < 0 || lz > 15 {
					continue
				}
				// Ore replaces stone only. Anything else means the vein ran
				// into air, a cave, water or the surface layers.
				existing := c.Block(uint8(lx), int16(by), uint8(lz), 0)
				if existing != o.stone && existing != o.deepslate {
					continue
				}
				if batch.discardOnAir > 0 && vr.float() < batch.discardOnAir && o.touchesAir(c, lx, by, lz, minY, maxY) {
					continue
				}
				out := rid
				if existing == o.deepslate {
					out = deepRID
				}
				c.SetBlock(uint8(lx), int16(by), uint8(lz), 0, out)
			}
		}
	}
}

// touchesAir reports whether any of the six neighbours of a position is air.
// Positions on the chunk border are only checked on the sides that stay inside
// the chunk, since a generator cannot read a neighbouring chunk.
func (o *Overworld) touchesAir(c *chunk.Chunk, lx, y, lz, minY, maxY int) bool {
	for _, d := range [6][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
		nx, ny, nz := lx+d[0], y+d[1], lz+d[2]
		if nx < 0 || nx > 15 || nz < 0 || nz > 15 || ny < minY || ny > maxY {
			continue
		}
		if c.Block(uint8(nx), int16(ny), uint8(nz), 0) == o.air {
			return true
		}
	}
	return false
}
