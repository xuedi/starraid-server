package game

import "github.com/xuedi/starraid-server/internal/catalog"

// Module is one module installed on an in-memory object: its mass plus the
// behaviour parameters the derivation reads (role inferred from which params are
// set — see catalog.ModuleParams).
type Module struct {
	Mass   int64
	Params catalog.ModuleParams
}

// Cargo is one cargo stack: per-unit mass × quantity, contributing to total mass.
type Cargo struct {
	UnitMass int64
	Quantity int64
}

// attrs are the attributes DERIVED from an object's class base + installed
// modules + cargo, cached on the object and recomputed on change (objects.md:
// "computed once, cached on change"). Movement reads maxSpeed; the rest are
// server-internal until a later HUD/protocol slice carries them.
type attrs struct {
	totalMass       int64
	maxSpeed        float64 // world units/s; 0 if no active thruster
	powerProduction float64
	powerDraw       float64 // actual draw after the overdraw cutoff
	shieldCapacity  float64 // 0 if the shield is cut by overdraw
	thrusterActive  bool
	shieldActive    bool
}

// recompute derives o.attrs from its base mass + modules + cargo. Pure over those
// inputs (no I/O, no clock). Power: generators produce, thruster/shield consume;
// if draw exceeds production, consumers are disabled by fixed priority — thruster
// first, then shield — until draw ≤ production (power.md overdraw cutoff). Speed
// is Σthrust / totalMass while the thruster is powered, else 0; a heavier (laden)
// object is therefore slower than an empty one.
func (o *Object) recompute() {
	mass := o.baseMass
	var production, thrust, thrusterDraw, shieldDraw, shieldCap float64
	for _, m := range o.modules {
		mass += m.Mass
		p := m.Params
		production += p.PowerOutput
		if p.Thrust > 0 {
			thrust += p.Thrust
			thrusterDraw += p.PowerDraw
		}
		if p.ShieldCapacity > 0 {
			shieldDraw += p.PowerDraw
			shieldCap += p.ShieldCapacity
		}
	}
	for _, c := range o.cargo {
		mass += c.UnitMass * c.Quantity
	}

	// Overdraw cutoff: cut the thruster first, then the shield, until the
	// remaining draw fits production.
	thrusterActive, shieldActive := true, true
	if thrusterDraw+shieldDraw > production {
		thrusterActive = false
		if shieldDraw > production {
			shieldActive = false
		}
	}

	o.attrs = attrs{
		totalMass:       mass,
		powerProduction: production,
		thrusterActive:  thrusterActive,
		shieldActive:    shieldActive,
	}
	if thrusterActive {
		o.attrs.powerDraw += thrusterDraw
		if mass > 0 {
			o.attrs.maxSpeed = thrust / float64(mass)
		}
	}
	if shieldActive {
		o.attrs.powerDraw += shieldDraw
		o.attrs.shieldCapacity = shieldCap
	}
	// TODO(combat): charge shield toward shieldCapacity at RechargeRate from
	// spare power; stamp object health/shield. Out of scope until combat lands.
}
