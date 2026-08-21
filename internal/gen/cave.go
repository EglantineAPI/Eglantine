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

// carvedAt reports whether a cell is hollow.
func (o *Overworld) carvedAt(x, y, z, height int, cc columnCave) bool {
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
	if y < cavernRoof {
		if o.cavern.fbm3(float64(x), float64(y)*1.5, float64(z), 2, 1.0/100.0, 0.5) > 0.16 {
			return true
		}
	}

	// Two independent fields intersected produce tunnels rather than blobs.
	// The threshold is what sets how much of the underground is hollow.
	const tunnel = 0.075
	a := o.cave.fbm3(float64(x), float64(y)*2.4, float64(z), 2, 1.0/48.0, 0.5)
	if absF(a) > tunnel {
		return false
	}
	b := o.cave.fbm3(float64(x)+700, float64(y)*2.4, float64(z)-700, 2, 1.0/48.0, 0.5)
	return absF(b) <= tunnel
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

// lushAt reports whether a position is inside one of the rare overgrown
// pockets. Lush caves are meant to be a find, so the threshold is high.
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
