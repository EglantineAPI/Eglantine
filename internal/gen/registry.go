// Package gen provides the world generators Eglantine can create worlds with.
//
// Dragonfly itself ships only a flat generator and a void generator, so the
// overworld, nether and end generators here are written from scratch on top of
// Perlin noise. They aim to be survivable and varied, not to reproduce vanilla
// generation: there are no structures, and a seed does not match the world the
// same seed produces in Bedrock.
package gen

import (
	"fmt"
	"sort"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/generator"
)

// Kind identifies one of the generators a world can be created with.
type Kind string

const (
	// KindOverworld is noise-generated survival terrain.
	KindOverworld Kind = "overworld"
	// KindFlat is the classic flat world: bedrock, dirt and grass.
	KindFlat Kind = "flat"
	// KindVoid is an entirely empty world.
	KindVoid Kind = "void"
	// KindNether is a netherrack cavern world with lava seas.
	KindNether Kind = "nether"
	// KindEnd is an end stone island world.
	KindEnd Kind = "end"
)

// kinds maps each Kind to the dimension its worlds run in. The dimension fixes
// the build range and the sky, so it cannot be chosen independently of the
// generator.
var kinds = map[Kind]world.Dimension{
	KindOverworld: world.Overworld,
	KindFlat:      world.Overworld,
	KindVoid:      world.Overworld,
	KindNether:    world.Nether,
	KindEnd:       world.End,
}

// Kinds returns every generator name, sorted. The /world command feeds this to
// the client as an enum so the names complete as they are typed.
func Kinds() []string {
	names := make([]string, 0, len(kinds))
	for k := range kinds {
		names = append(names, string(k))
	}
	sort.Strings(names)
	return names
}

// ParseKind resolves a generator name, reporting whether it is known.
func ParseKind(s string) (Kind, bool) {
	k := Kind(s)
	_, ok := kinds[k]
	return k, ok
}

// Dimension returns the dimension worlds of this Kind run in.
func (k Kind) Dimension() world.Dimension {
	dim, ok := kinds[k]
	if !ok {
		return world.Overworld
	}
	return dim
}

// NewPooled builds the generator for this Kind and routes its chunk generation
// through the pool passed, so that generation across every world stays bounded.
// A nil pool leaves the generator unwrapped.
func (k Kind) NewPooled(seed int64, p *Pool) (world.Generator, error) {
	g, err := k.New(seed)
	if err != nil {
		return nil, err
	}
	return Pooled(g, p), nil
}

// New builds the generator for this Kind at the seed passed.
func (k Kind) New(seed int64) (world.Generator, error) {
	switch k {
	case KindOverworld:
		return NewOverworld(seed), nil
	case KindNether:
		return NewNether(seed), nil
	case KindEnd:
		return NewEnd(seed), nil
	case KindVoid:
		return world.NopGenerator{}, nil
	case KindFlat:
		return generator.NewFlat(biome.Plains{}, []world.Block{
			block.Grass{},
			block.Dirt{},
			block.Dirt{},
			block.Bedrock{},
		}), nil
	}
	return nil, fmt.Errorf("unknown generator %q", string(k))
}
