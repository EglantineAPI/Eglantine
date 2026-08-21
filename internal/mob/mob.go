// Package mob adds mob entities to the server.
//
// Dragonfly ships no mobs at all: its entity package covers projectiles,
// dropped items, TNT and the like, and nothing that walks. The mobs here are
// deliberately inert. They fall, they can be hit, hurt and killed, and they
// show up correctly on the client because they encode to the vanilla entity
// names the client already knows. They have no AI and never spawn on their own;
// something has to place them.
package mob

import (
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// deathLinger is how long a corpse stays before it is removed, giving the death
// animation time to play on the client.
const deathLinger = time.Millisecond * 1100

// Type is a world.EntityType for one kind of mob.
type Type struct {
	// encoded is the vanilla entity name, such as "minecraft:zombie". Using the
	// vanilla name is what makes the client render a real model rather than
	// nothing at all.
	encoded string
	// width and height are the bounding box in blocks.
	width, height float64
	// maxHealth is the health a freshly placed mob has.
	maxHealth float64
	// fireImmune covers the mobs that ignore burning, such as those native to
	// the nether.
	fireImmune bool
}

// Name returns the vanilla entity name of the type.
func (t Type) Name() string { return t.encoded }

// MaxHealth returns the health a mob of this type starts with.
func (t Type) MaxHealth() float64 { return t.maxHealth }

// EncodeEntity implements world.EntityType.
func (t Type) EncodeEntity() string { return t.encoded }

// BBox implements world.EntityType.
func (t Type) BBox(world.Entity) cube.BBox {
	half := t.width / 2
	return cube.Box(-half, 0, -half, half, t.height, half)
}

// Open implements world.EntityType.
func (t Type) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &Mob{Ent: entity.Open(tx, handle, data), t: t, tx: tx}
}

// DecodeNBT implements world.EntityType.
func (t Type) DecodeNBT(m map[string]any, data *world.EntityData) {
	health := t.maxHealth
	if v, ok := m["Health"].(float32); ok && float64(v) > 0 {
		health = float64(v)
	}
	data.Data = newState(t, health)
}

// EncodeNBT implements world.EntityType.
func (t Type) EncodeNBT(data *world.EntityData) map[string]any {
	s, ok := data.Data.(*state)
	if !ok {
		return nil
	}
	return map[string]any{"Health": float32(s.health.Health())}
}

// state is the per-mob data carried across transactions.
//
// It doubles as the entity's behaviour, which is what Dragonfly ticks. The
// embedded PassiveBehaviour is the whole of that behaviour: it applies gravity
// and drag and nothing else, which is exactly a mob with no AI.
type state struct {
	*entity.PassiveBehaviour

	health  *entity.HealthManager
	effects *entity.EffectManager
	speed   float64
	// dying marks a mob whose death has already been announced, so the death
	// animation is not sent twice while the corpse lingers.
	dying bool
}

func newState(t Type, health float64) *state {
	return &state{
		PassiveBehaviour: entity.PassiveBehaviourConfig{
			Gravity: 0.08,
			Drag:    0.02,
		}.New(),
		health:  entity.NewHealthManager(health, t.maxHealth),
		effects: entity.NewEffectManager(),
		speed:   0.1,
	}
}

// Mob is a living entity with no AI.
type Mob struct {
	*entity.Ent
	t  Type
	tx *world.Tx
}

// state returns the mob's data.
func (m *Mob) state() *state { return m.Ent.Behaviour().(*state) }

// Type returns the kind of mob this is.
func (m *Mob) Type() Type { return m.t }

// Health returns the mob's current health.
func (m *Mob) Health() float64 { return m.state().health.Health() }

// MaxHealth returns the mob's maximum health.
func (m *Mob) MaxHealth() float64 { return m.state().health.MaxHealth() }

// SetMaxHealth sets the mob's maximum health.
func (m *Mob) SetMaxHealth(v float64) { m.state().health.SetMaxHealth(v) }

// Dead reports whether the mob has run out of health.
func (m *Mob) Dead() bool { return m.Health() <= 0 }

// Speed returns the mob's movement speed. Nothing moves a mob without AI, but
// effects and other code read it.
func (m *Mob) Speed() float64 { return m.state().speed }

// SetSpeed sets the mob's movement speed.
func (m *Mob) SetSpeed(v float64) { m.state().speed = v }

// AddEffect applies a status effect to the mob.
func (m *Mob) AddEffect(e effect.Effect) { m.state().effects.Add(e, m) }

// RemoveEffect removes a status effect from the mob.
func (m *Mob) RemoveEffect(e effect.Type) { m.state().effects.Remove(e, m) }

// Effects returns the status effects on the mob.
func (m *Mob) Effects() []effect.Effect { return m.state().effects.Effects() }

// Heal restores health, up to the mob's maximum.
func (m *Mob) Heal(health float64, _ world.HealingSource) float64 {
	if m.Dead() || health < 0 {
		return 0
	}
	before := m.Health()
	m.state().health.AddHealth(health)
	return m.Health() - before
}

// Hurt damages the mob, returning how much health it actually lost.
//
// A mob that reaches zero health plays its death animation and is removed a
// moment later, rather than vanishing the instant it is hit.
func (m *Mob) Hurt(damage float64, src world.DamageSource) (float64, bool) {
	s := m.state()
	if m.Dead() || damage < 0 {
		return 0, false
	}
	if m.t.fireImmune && src.Fire() {
		return 0, false
	}

	before := m.Health()
	s.health.AddHealth(-damage)
	dealt := before - m.Health()

	pos := m.Position()
	for _, v := range m.tx.Viewers(pos) {
		v.ViewEntityAction(m, entity.HurtAction{})
	}
	if m.Dead() && !s.dying {
		s.dying = true
		m.die()
	}
	return dealt, true
}

// die plays the death animation and schedules the corpse's removal.
func (m *Mob) die() {
	pos := m.Position()
	for _, v := range m.tx.Viewers(pos) {
		v.ViewEntityAction(m, entity.DeathAction{})
	}
	handle := m.H()
	m.tx.World().DoAfter(deathLinger, func(tx *world.Tx) {
		if e, ok := handle.Entity(tx); ok {
			_ = e.Close()
		}
	})
}

// KnockBack pushes the mob away from a position.
func (m *Mob) KnockBack(src mgl64.Vec3, force, height float64) {
	if m.Dead() {
		return
	}
	dir := m.Position().Sub(src)
	dir[1] = 0
	if dir.Len() == 0 {
		dir = mgl64.Vec3{0, 0, 1}
	}
	vel := dir.Normalize().Mul(force)
	vel[1] = height
	m.SetVelocity(vel)
}

// Mob has to satisfy entity.Living, which is what makes it attackable: the
// player's attack path looks for that interface before anything else.
var _ entity.Living = (*Mob)(nil)
