package gen

import "math"

// perlin is a seeded 3D Perlin noise source. A perlin value is immutable once
// built by newPerlin, so it is safe for the concurrent GenerateChunk calls
// Dragonfly makes when ChunkLoadWorkers is above one.
type perlin struct {
	// p is the classic doubled permutation table. Doubling it lets the lookups
	// in noise3 index p[i+1] without a bounds check or a modulo.
	p [512]int
}

// newPerlin builds a noise source from a seed. The same seed always yields the
// same permutation, so a world regenerates identically across restarts.
func newPerlin(seed int64) *perlin {
	var pn perlin
	for i := range 256 {
		pn.p[i] = i
	}
	// Fisher-Yates driven by a splitmix64 stream rather than math/rand, so the
	// shuffle does not depend on the global source or on the Go version.
	r := uint64(seed)
	for i := 255; i > 0; i-- {
		r += 0x9e3779b97f4a7c15
		z := r
		z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
		z = (z ^ (z >> 27)) * 0x94d049bb133111eb
		z ^= z >> 31
		j := int(z % uint64(i+1))
		pn.p[i], pn.p[j] = pn.p[j], pn.p[i]
	}
	for i := range 256 {
		pn.p[256+i] = pn.p[i]
	}
	return &pn
}

func fade(t float64) float64 { return t * t * t * (t*(t*6-15) + 10) }

func lerp(t, a, b float64) float64 { return a + t*(b-a) }

// grad returns the dot product of a pseudo-random gradient vector with the
// distance vector, using Perlin's improved-noise gradient selection.
func grad(hash int, x, y, z float64) float64 {
	h := hash & 15
	u := x
	if h >= 8 {
		u = y
	}
	v := y
	if h >= 4 {
		v = z
		if h == 12 || h == 14 {
			v = x
		}
	}
	if h&1 != 0 {
		u = -u
	}
	if h&2 != 0 {
		v = -v
	}
	return u + v
}

// noise3 returns Perlin noise at the point passed, in roughly [-1, 1].
func (pn *perlin) noise3(x, y, z float64) float64 {
	xi := int(math.Floor(x)) & 255
	yi := int(math.Floor(y)) & 255
	zi := int(math.Floor(z)) & 255
	x -= math.Floor(x)
	y -= math.Floor(y)
	z -= math.Floor(z)
	u, v, w := fade(x), fade(y), fade(z)

	a := pn.p[xi] + yi
	aa := pn.p[a] + zi
	ab := pn.p[a+1] + zi
	b := pn.p[xi+1] + yi
	ba := pn.p[b] + zi
	bb := pn.p[b+1] + zi

	return lerp(w,
		lerp(v,
			lerp(u, grad(pn.p[aa], x, y, z), grad(pn.p[ba], x-1, y, z)),
			lerp(u, grad(pn.p[ab], x, y-1, z), grad(pn.p[bb], x-1, y-1, z)),
		),
		lerp(v,
			lerp(u, grad(pn.p[aa+1], x, y, z-1), grad(pn.p[ba+1], x-1, y, z-1)),
			lerp(u, grad(pn.p[ab+1], x, y-1, z-1), grad(pn.p[bb+1], x-1, y-1, z-1)),
		),
	)
}

// noise2 returns 2D Perlin noise, used for heightmaps and climate fields.
func (pn *perlin) noise2(x, z float64) float64 { return pn.noise3(x, 0, z) }

// fbm sums octaves of 2D noise, each at double the frequency and half the
// amplitude of the last. The result is normalised back to roughly [-1, 1].
func (pn *perlin) fbm(x, z float64, octaves int, freq, persistence float64) float64 {
	var total, amp, max float64 = 0, 1, 0
	for range octaves {
		total += pn.noise2(x*freq, z*freq) * amp
		max += amp
		amp *= persistence
		freq *= 2
	}
	if max == 0 {
		return 0
	}
	return total / max
}

// fbm3 is fbm over 3D noise, used to carve caves.
func (pn *perlin) fbm3(x, y, z float64, octaves int, freq, persistence float64) float64 {
	var total, amp, max float64 = 0, 1, 0
	for range octaves {
		total += pn.noise3(x*freq, y*freq, z*freq) * amp
		max += amp
		amp *= persistence
		freq *= 2
	}
	if max == 0 {
		return 0
	}
	return total / max
}

// hashMix mixes a coordinate triple and a seed into a well-distributed uint64.
// Generators use it for decisions that must depend only on position and seed,
// never on evaluation order, since Dragonfly may call GenerateChunk
// concurrently.
func hashMix(x, y, z int, seed uint64) uint64 {
	h := uint64(x)*0x9e3779b97f4a7c15 ^ uint64(z)*0xc2b2ae3d27d4eb4f
	h ^= uint64(y)*0x165667b19e3779f9 + seed
	h ^= h >> 33
	h *= 0xff51afd7ed558ccd
	h ^= h >> 33
	h *= 0xc4ceb9fe1a85ec53
	h ^= h >> 33
	return h
}
