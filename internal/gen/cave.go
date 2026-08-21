package gen

// The cave system has four parts, each answering a different complaint about a
// world made only of winding tunnels:
//
//   - tunnels, the thin winding passages, now sparser than they were;
//   - caverns, large open chambers that exist only below y=0;
//   - ravines, tall narrow slots that cut up through the terrain and usually
//     break the surface;
//   - lush pockets, rare patches where a cave floor is mossy and overgrown.
//
// Everything is derived from noise and position, so a chunk carves the same way
// however many times it is generated and whichever worker generates it.

// Vertical limits shared by the carvers. Nothing carves into the bedrock floor.
const (
	caveFloor   = -59
	cavernRoof  = 0
	ravineFloor = -52
)

// columnCave is the part of the cave system that depends only on a column.
// Computing it once per column rather than once per cell keeps the 2D noise
// lookups out of the inner loop.
type columnCave struct {
	// openSurface allows tunnels to reach the top of the terrain here, which is
	// how a cave mouth appears on a hillside instead of every cave being sealed.
	openSurface bool
	// ravine is set when the column falls inside a ravine slot.
	ravine bool
	// ravineTop and ravineBottom bound the slot, inclusive.
	ravineTop, ravineBottom int
}

// columnCaveAt computes the per-column cave state for a position.
func (o *Overworld) columnCaveAt(x, z, height int) columnCave {
	var cc columnCave

	// Cave mouths are deliberately uncommon: this mask is above its threshold
	// over roughly a tenth of the surface.
	cc.openSurface = o.cave.fbm(float64(x)+4400, float64(z)-4400, 2, 1.0/260.0, 0.5) > 0.16

	// A ravine needs two things: a region that has ravines at all, and a narrow
	// line within it. Without the region mask, the line field would draw
	// ravines evenly across the entire world.
	if o.ravineRegion(x, z) {
		line := o.cave.fbm(float64(x)-9000, float64(z)+9000, 2, 1.0/380.0, 0.5)
		if halfWidth := 0.019; absF(line) < halfWidth {
			cc.ravine = true
			// Ravines open at the surface, which is what makes them a landmark
			// rather than a hidden void.
			cc.ravineTop = height - 2
			depth := 28 + int(o.hash(x/64, 0, z/64, 0x5a1e)%26)
			cc.ravineBottom = maxInt(ravineFloor, cc.ravineTop-depth)
		}
	}
	return cc
}

// ravineRegion reports whether ravines occur near a position at all.
func (o *Overworld) ravineRegion(x, z int) bool {
	return o.ravine.fbm(float64(x)+2100, float64(z)+2100, 1, 1.0/1500.0, 0.5) > 0.15
}

// Cave noise is sampled on a lattice and interpolated rather than evaluated at
// every block. The fields have a wavelength of roughly fifty blocks, so they
// barely change over four, and evaluating them per block was the single largest
// cost in world generation: a chunk has around 33,000 solid blocks and each one
// was costing four to six Perlin evaluations.
// caveStride is the lattice spacing in blocks, on all three axes.
const caveStride = 4

// caveField holds the cave noise for one chunk on a coarse lattice.
type caveField struct {
	// tunnelA and tunnelB are the two fields whose intersection makes tunnels.
	// cavern is the large-chamber field, sampled only below cavernRoof.
	tunnelA, tunnelB, cavern []float32

	x0, y0, z0 int
	nx, ny, nz int
	cavernRows int
}

// newCaveField samples the cave noise around a chunk, up to the highest ground
// in it. The lattice extends one point past the chunk on each side so every
// block inside it can interpolate.
//
// Bounding it by the terrain matters: nothing above the surface is ever carved,
// so sampling a fixed height for every chunk spent most of the cave noise on
// open sky. A chunk whose ground tops out at y=80 needs a third of the rows a
// mountain chunk does.
func (o *Overworld) newCaveField(pos chunkPos, topY int) *caveField {
	f := &caveField{
		x0: pos.x * 16, z0: pos.z * 16, y0: caveFloor,
		nx: 16/caveStride + 1, nz: 16/caveStride + 1,
	}
	if topY < cavernRoof {
		topY = cavernRoof
	}
	f.ny = (topY-f.y0)/caveStride + 2
	f.cavernRows = (cavernRoof-f.y0)/caveStride + 2

	f.tunnelA = make([]float32, f.nx*f.ny*f.nz)
	f.tunnelB = make([]float32, f.nx*f.ny*f.nz)
	f.cavern = make([]float32, f.nx*f.cavernRows*f.nz)

	for ix := range f.nx {
		wx := float64(f.x0 + ix*caveStride)
		for iz := range f.nz {
			wz := float64(f.z0 + iz*caveStride)
			for iy := range f.ny {
				wy := float64(f.y0+iy*caveStride) * 2.4
				i := f.index(ix, iy, iz, f.ny)
				f.tunnelA[i] = float32(o.cave.fbm3(wx, wy, wz, 2, 1.0/48.0, 0.5))
				f.tunnelB[i] = float32(o.cave.fbm3(wx+700, wy, wz-700, 2, 1.0/48.0, 0.5))
			}
			for iy := range f.cavernRows {
				wy := float64(f.y0+iy*caveStride) * 1.5
				f.cavern[f.index(ix, iy, iz, f.cavernRows)] =
					float32(o.cavern.fbm3(wx, wy, wz, 2, 1.0/100.0, 0.5))
			}
		}
	}
	return f
}

func (f *caveField) index(ix, iy, iz, rows int) int { return (ix*rows+iy)*f.nz + iz }

// caveColumn is the cave field reduced to a single column.
//
// Walking a column, x and z never change, so the horizontal interpolation is
// the same for every block in it. Doing it once per column instead of once per
// block turns eight lattice lookups and seven interpolations per block into one
// interpolation.
type caveColumn struct {
	tunnelA, tunnelB, cavern []float32
	y0, cavernRows           int
}

// column reduces the field to the column at the world position passed.
func (f *caveField) column(x, z int, buf *caveColumn) {
	fx := float64(x-f.x0) / caveStride
	fz := float64(z-f.z0) / caveStride
	ix, iz := int(fx), int(fz)
	tx, tz := fx-float64(ix), fz-float64(iz)
	if ix+1 >= f.nx || iz+1 >= f.nz || ix < 0 || iz < 0 {
		buf.tunnelA, buf.tunnelB, buf.cavern = nil, nil, nil
		return
	}

	buf.y0, buf.cavernRows = f.y0, f.cavernRows
	buf.tunnelA = f.flatten(f.tunnelA, f.ny, ix, iz, tx, tz, buf.tunnelA)
	buf.tunnelB = f.flatten(f.tunnelB, f.ny, ix, iz, tx, tz, buf.tunnelB)
	buf.cavern = f.flatten(f.cavern, f.cavernRows, ix, iz, tx, tz, buf.cavern)
}

// flatten interpolates one lattice horizontally into a column of rows, reusing
// the destination slice so a chunk allocates these once rather than 256 times.
func (f *caveField) flatten(values []float32, rows, ix, iz int, tx, tz float64, dst []float32) []float32 {
	if cap(dst) < rows {
		dst = make([]float32, rows)
	}
	dst = dst[:rows]
	for iy := range rows {
		c00 := values[f.index(ix, iy, iz, rows)]
		c10 := values[f.index(ix+1, iy, iz, rows)]
		c01 := values[f.index(ix, iy, iz+1, rows)]
		c11 := values[f.index(ix+1, iy, iz+1, rows)]
		a := float64(c00) + tx*float64(c10-c00)
		b := float64(c01) + tx*float64(c11-c01)
		dst[iy] = float32(a + tz*(b-a))
	}
	return dst
}

// at interpolates a flattened column at a height.
func (col *caveColumn) at(values []float32, rows, y int) float64 {
	fy := float64(y-col.y0) / caveStride
	iy := int(fy)
	if iy < 0 || iy+1 >= rows || len(values) == 0 {
		return 0
	}
	lo, hi := float64(values[iy]), float64(values[iy+1])
	return lo + (fy-float64(iy))*(hi-lo)
}

// carvedAt reports whether a cell is hollow.
func (o *Overworld) carvedAt(col *caveColumn, x, y, z, height int, cc columnCave) bool {
	if y < caveFloor {
		return false
	}
	// Ravines are checked first and cheaply: the column already knows its slot,
	// so this is two comparisons and one width test.
	if cc.ravine && y <= cc.ravineTop && y >= cc.ravineBottom {
		if o.inRavine(x, y, z, cc) {
			return true
		}
	}

	// Tunnels normally stop short of the surface so the ground is not
	// perforated from above. Where the opening mask allows it they run all the
	// way up and daylight gets in.
	ceiling := height - 6
	if cc.openSurface {
		ceiling = height - 1
	}
	if y > ceiling {
		return false
	}

	// Large chambers, only in the deepslate layer. One noise field with a high
	// threshold gives a few big rooms rather than many small ones.
	if y < cavernRoof && col.at(col.cavern, col.cavernRows, y) > 0.16 {
		return true
	}

	// Two independent fields intersected produce tunnels rather than blobs.
	// The threshold is what sets how much of the underground is hollow.
	const tunnel = 0.075
	if absF(col.at(col.tunnelA, len(col.tunnelA), y)) > tunnel {
		return false
	}
	return absF(col.at(col.tunnelB, len(col.tunnelB), y)) <= tunnel
}

// inRavine reports whether a cell falls inside the ravine slot, which is
// widest in the middle of its height and pinches shut at both ends.
func (o *Overworld) inRavine(x, y, z int, cc columnCave) bool {
	span := cc.ravineTop - cc.ravineBottom
	if span <= 0 {
		return false
	}
	// Position within the slot, 0 at the bottom and 1 at the top.
	t := float64(y-cc.ravineBottom) / float64(span)
	// Pinch the very bottom and leave the top open, so the slot looks cut from
	// above rather than like a floating slab.
	if t < 0.15 {
		return absF(o.cave.fbm(float64(x)*3, float64(z)*3, 1, 1.0/40.0, 0.5)) < 0.25*t/0.15
	}
	return true
}

// lushRegion reports whether a column is anywhere near a lush pocket.
//
// It is a cheap two-dimensional test used to skip the expensive one. Scanning
// every column of every chunk from the surface to bedrock calling lushAt was
// costing a fifth of all world generation time, to find something that exists
// in a small fraction of columns.
func (o *Overworld) lushRegion(x, z int) bool {
	return o.lush.fbm(float64(x), float64(z), 1, 1.0/300.0, 0.5) > 0.18
}

// lushAt reports whether a position is inside one of the rare overgrown
// pockets. Lush caves are meant to be a find, so the threshold is high.
//
// Callers scanning a column should test lushRegion first.
func (o *Overworld) lushAt(x, y, z int) bool {
	if y > 24 || y < caveFloor {
		return false
	}
	return o.lush.fbm3(float64(x), float64(y)*1.2, float64(z), 2, 1.0/120.0, 0.5) > 0.46
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
