package gen

import (
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// Pool bounds how many chunks are generated at the same time across the whole
// server.
//
// Dragonfly gives every world its own chunk workers, and those workers do two
// different jobs: reading chunks off disk, which waits on I/O, and generating
// new ones, which does not — it is pure arithmetic and will use a core for as
// long as it is given one. With several worlds open, each with its own workers,
// nothing stops generation from occupying every core at once and starving the
// goroutines that actually tick the worlds. That is what a joining player
// experiences as the server freezing.
//
// Routing generation through one pool separates the two. Worlds can keep plenty
// of workers so disk loads stay parallel, while the amount of generation
// running at any moment stays fixed no matter how many worlds are open.
type Pool struct {
	jobs   chan func()
	done   chan struct{}
	closed sync.Once
	wg     sync.WaitGroup
	size   int
}

// NewPool starts a pool with the given number of workers. A size below one is
// raised to one.
func NewPool(size int) *Pool {
	if size < 1 {
		size = 1
	}
	p := &Pool{jobs: make(chan func()), done: make(chan struct{}), size: size}
	p.wg.Add(size)
	for range size {
		go func() {
			defer p.wg.Done()
			for {
				select {
				case job := <-p.jobs:
					job()
				case <-p.done:
					return
				}
			}
		}()
	}
	return p
}

// Size returns how many chunks the pool generates at once.
func (p *Pool) Size() int { return p.size }

// run hands f to a worker and waits for it to finish.
//
// The caller blocks, which is the point: it is a chunk worker that has nothing
// else to do until the chunk exists, and blocking it is what limits how much
// generation runs at once.
//
// f must never itself call run. Nothing in generation does, and a nested call
// would deadlock once every worker was waiting on one.
func (p *Pool) run(f func()) {
	finished := make(chan struct{})
	job := func() {
		defer close(finished)
		f()
	}
	// The jobs channel is deliberately never closed. Closing it would make the
	// send below a panic rather than a blocked operation, and a select cannot
	// avoid that: a send on a closed channel counts as ready, so the race is
	// unwinnable. Shutdown is signalled on its own channel instead.
	select {
	case p.jobs <- job:
		<-finished
	case <-p.done:
		// The pool is shutting down. Run the work here rather than dropping a
		// chunk on the floor, which would leave a hole in the world.
		f()
	}
}

// Close stops the pool's workers. Generation after this point runs inline on
// the caller, so a world closing down still finishes any chunk it is mid-way
// through. Calling Close more than once is safe.
func (p *Pool) Close() {
	p.closed.Do(func() {
		close(p.done)
		p.wg.Wait()
	})
}

// Pooled wraps a generator so that its chunk generation runs on the pool passed.
// A nil pool returns the generator unchanged.
func Pooled(g world.Generator, p *Pool) world.Generator {
	if p == nil {
		return g
	}
	return pooledGenerator{g: g, p: p}
}

// pooledGenerator is the wrapper Pooled returns.
type pooledGenerator struct {
	g world.Generator
	p *Pool
}

// GenerateChunk implements world.Generator, running the real generator on the
// pool.
func (pg pooledGenerator) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	pg.p.run(func() { pg.g.GenerateChunk(pos, c) })
}

// DefaultSpawn implements world.Generator. Finding a spawn is a one-off and
// does not go through the pool.
func (pg pooledGenerator) DefaultSpawn(dim world.Dimension) cube.Pos {
	return pg.g.DefaultSpawn(dim)
}
