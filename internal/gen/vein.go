package gen

import (
	"math"

	"github.com/df-mc/dragonfly/server/world/chunk"
)

// This file is the ore placement engine, shared by the overworld and the
// nether. Both follow vanilla's model: ore is placed per chunk, not per block.
// For each batch the generator makes `count` attempts, each dropping one vein
// of up to `size` blocks at a height drawn from the batch's distribution.
//
// Deciding ore per block instead — rolling a probability at every stone
// position — is what produces a world where ore is everywhere and rare ore is
// not rare.

// oreDensity scales every vein count.
//
// This is a deliberate departure from vanilla's table, and the one number to
// change if the server feels too generous or too mean. Vanilla's counts assume
// vanilla's terrain: far more cave wall, aquifers cutting through the deep
// layers, and a thicker stone column overall. Transplanted unchanged into this
// generator they read as an ore-rich world, so the whole table is scaled back.
const oreDensity = 0.55

// wallExposure is the share of deep stone that touches a cave wall in vanilla,
// standing in for the wall area this generator does not have.
//
// Vanilla culls an ore block when it touches air, which is what keeps gold,
// lapis and diamond scarce. Measured here, under a tenth of deep stone touches
// air against roughly half in vanilla, because this generator trades many
// winding tunnels for fewer, larger caverns and a large cavern has far less
// wall per unit volume. Without this the buried batches survive almost intact.
const wallExposure = 0.72

// heightDist is how a batch draws the y of a vein.
type heightDist int

const (
	// distUniform spreads veins evenly across the range.
	distUniform heightDist = iota
	// distTriangle peaks in the middle of the range and falls off linearly to
	// both edges. Vanilla calls this a trapezoid with no plateau.
	distTriangle
)

// veinSpec is one entry of an ore table.
type veinSpec struct {
	// block is placed in ordinary host rock; deep is used where the host is the
	// deeper variant, and may be zero when there is none.
	block, deep uint32
	// count is the number of veins attempted per chunk before scaling.
	count int
	// size is the number of steps a vein is shaped over.
	size int
	dist heightDist
	// minY and maxY bound the vein centre, inclusive. A range extending below
	// the world is meaningful: the attempts that fall outside are discarded,
	// which is how vanilla makes diamond rare.
	minY, maxY int
	// discardOnAir is the chance of skipping a block that touches air.
	discardOnAir float64
	// scattered places single blocks rather than a blob, the way vanilla
	// generates ancient debris.
	scattered bool
	// mountainOnly restricts the batch to peak biomes, as vanilla does for
	// emerald.
	mountainOnly bool
}

// veinField places an ore table into chunks.
type veinField struct {
	seed  int64
	specs []veinSpec
	// hostRock reports whether ore may replace a block, and whether that block
	// is the deep variant.
	hostRock func(rid uint32) (deep, ok bool)
	air      uint32
}

// maxVeinReach bounds how far a vein can extend from its centre, deciding how
// many neighbouring chunks have to be considered so veins are not clipped at
// chunk borders.
const maxVeinReach = 5

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
func (v veinSpec) pick(r *detRand) int {
	span := v.maxY - v.minY + 1
	if span <= 1 {
		return v.minY
	}
	if v.dist == distUniform {
		return v.minY + r.intn(span)
	}
	// Averaging two uniform draws gives the triangular distribution vanilla
	// produces from a trapezoid with no plateau.
	return v.minY + (r.intn(span)+r.intn(span))/2
}

// scaledCount applies oreDensity, keeping the fractional part as a probability
// so a batch of one attempt is thinned rather than removed outright.
func scaledCount(count int, r *detRand) int {
	exact := float64(count) * oreDensity
	n := int(exact)
	if r.float() < exact-float64(n) {
		n++
	}
	return n
}

// place runs every batch for the chunk at pos, including veins originating in
// the eight neighbouring chunks whose blobs reach across the border.
func (f *veinField) place(pos chunkPos, c *chunk.Chunk, mountains bool) {
	minY, maxY := int(c.Range().Min()), int(c.Range().Max())
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			f.placeFrom(pos.x+dx, pos.z+dz, pos, c, mountains, minY, maxY)
		}
	}
}

// placeFrom places the veins belonging to chunk (ox, oz), writing only the
// blocks that land inside the chunk at pos.
func (f *veinField) placeFrom(ox, oz int, pos chunkPos, c *chunk.Chunk, mountains bool, minY, maxY int) {
	baseX, baseZ := ox*16, oz*16
	wx0, wz0 := pos.x*16, pos.z*16

	for i, spec := range f.specs {
		if spec.mountainOnly && !mountains {
			// Emerald makes a hundred attempts per chunk; skipping the whole
			// batch where no peak biome exists saves the bulk of that work.
			continue
		}
		r := &detRand{s: hashMix(ox, i, oz, uint64(f.seed)+0x0e5a)}
		attempts := scaledCount(spec.count, r)
		for range attempts {
			cx := baseX + r.intn(16)
			cz := baseZ + r.intn(16)
			cy := spec.pick(r)
			seed := r.next()

			// Reject veins that cannot reach this chunk before doing the work
			// of shaping one. The stream has already advanced, so rejecting
			// does not shift the veins that follow.
			if cx+maxVeinReach < wx0 || cx-maxVeinReach >= wx0+16 ||
				cz+maxVeinReach < wz0 || cz-maxVeinReach >= wz0+16 ||
				cy+maxVeinReach < minY || cy-maxVeinReach > maxY {
				continue
			}
			f.placeVein(spec, &detRand{s: seed}, c, cx, cy, cz, wx0, wz0, minY, maxY)
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
func (f *veinField) placeVein(spec veinSpec, vr *detRand, c *chunk.Chunk, cx, cy, cz, wx0, wz0, minY, maxY int) {
	if spec.scattered {
		// Scattered ore places single blocks around a point rather than a
		// connected blob, which is what makes ancient debris turn up alone.
		for range spec.size {
			bx := cx + vr.intn(5) - 2
			by := cy + vr.intn(5) - 2
			bz := cz + vr.intn(5) - 2
			f.set(spec, vr, c, bx, by, bz, wx0, wz0, minY, maxY)
		}
		return
	}

	angle := vr.float() * math.Pi
	spread := float64(spec.size) / 8

	x0 := float64(cx) + math.Sin(angle)*spread
	x1 := float64(cx) - math.Sin(angle)*spread
	z0 := float64(cz) + math.Cos(angle)*spread
	z1 := float64(cz) - math.Cos(angle)*spread
	y0 := float64(cy + vr.intn(3) - 2)
	y1 := float64(cy + vr.intn(3) - 2)

	for k := range spec.size {
		t := float64(k) / float64(spec.size)
		px := x0 + (x1-x0)*t
		py := y0 + (y1-y0)*t
		pz := z0 + (z1-z0)*t

		// Radius tapers with a sine so the vein is thickest in the middle.
		scale := vr.float() * float64(spec.size) / 16
		radius := ((math.Sin(math.Pi*t)+1)*scale + 1) / 2
		f.fillBlob(spec, vr, c, px, py, pz, radius, wx0, wz0, minY, maxY)
	}
}

// fillBlob places ore in the sphere of the radius passed around a point.
func (f *veinField) fillBlob(spec veinSpec, vr *detRand, c *chunk.Chunk, px, py, pz, radius float64, wx0, wz0, minY, maxY int) {
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
			if dx*dx+dy*dy >= 1 {
				continue
			}
			for bz := zLo; bz <= zHi; bz++ {
				dz := (float64(bz) + 0.5 - pz) / radius
				if dx*dx+dy*dy+dz*dz >= 1 {
					continue
				}
				f.set(spec, vr, c, bx, by, bz, wx0, wz0, minY, maxY)
			}
		}
	}
}

// set writes one ore block, if the position holds host rock.
func (f *veinField) set(spec veinSpec, vr *detRand, c *chunk.Chunk, bx, by, bz, wx0, wz0, minY, maxY int) {
	if by < minY || by > maxY {
		return
	}
	lx, lz := bx-wx0, bz-wz0
	if lx < 0 || lx > 15 || lz < 0 || lz > 15 {
		return
	}
	deep, ok := f.hostRock(c.Block(uint8(lx), int16(by), uint8(lz), 0))
	if !ok {
		return
	}
	if spec.discardOnAir > 0 && vr.float() < spec.discardOnAir {
		// The air test keeps buried ore out of cave walls, which is what it
		// looks like it is for. The second roll culls at the rate vanilla's
		// extra wall area would have, which is what actually keeps the rare
		// ores rare here.
		if f.touchesAir(c, lx, by, lz, minY, maxY) {
			return
		}
		if vr.float() < wallExposure {
			return
		}
	}
	out := spec.block
	if deep && spec.deep != 0 {
		out = spec.deep
	}
	c.SetBlock(uint8(lx), int16(by), uint8(lz), 0, out)
}

// touchesAir reports whether any of the six neighbours of a position is air.
// Positions on the chunk border are only checked on the sides that stay inside
// the chunk, since a generator cannot read a neighbouring chunk.
func (f *veinField) touchesAir(c *chunk.Chunk, lx, y, lz, minY, maxY int) bool {
	for _, d := range [6][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
		nx, ny, nz := lx+d[0], y+d[1], lz+d[2]
		if nx < 0 || nx > 15 || nz < 0 || nz > 15 || ny < minY || ny > maxY {
			continue
		}
		if c.Block(uint8(nx), int16(ny), uint8(nz), 0) == f.air {
			return true
		}
	}
	return false
}
