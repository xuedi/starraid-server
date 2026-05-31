package game

import (
	"testing"

	"github.com/xuedi/starraid-server/internal/catalog"
)

// The placeholder skiff fitting (catalog.go): a basic hydrogen generator, a basic
// shield, and an ion thruster. Power-positive: production 100 ≥ draw 40+30.
func skiffModules() []Module {
	return []Module{
		{Mass: 200, Params: catalog.ModuleParams{PowerOutput: 100}},
		{Mass: 150, Params: catalog.ModuleParams{ShieldCapacity: 500, PowerDraw: 30}},
		{Mass: 100, Params: catalog.ModuleParams{Thrust: 250000, PowerDraw: 40}},
	}
}

const skiffBaseMass = 1000 // catalog skiff base_mass

// fit builds and derives an object with the given base mass, modules and cargo.
func fit(baseMass int64, mods []Module, cargo []Cargo) *Object {
	o := &Object{baseMass: baseMass, modules: mods, cargo: cargo}
	o.recompute()
	return o
}

func TestDerivedMassIncludesModulesAndCargo(t *testing.T) {
	o := fit(skiffBaseMass, skiffModules(), []Cargo{{UnitMass: 1, Quantity: 100}})
	// 1000 base + (200+150+100) modules + 1*100 cargo = 1550.
	if o.attrs.totalMass != 1550 {
		t.Fatalf("total mass: want 1550, got %d", o.attrs.totalMass)
	}
}

func TestPowerPositiveFittingRuns(t *testing.T) {
	o := fit(skiffBaseMass, skiffModules(), nil)
	if !o.attrs.thrusterActive || !o.attrs.shieldActive {
		t.Fatalf("power-positive skiff: want thruster+shield active, got thruster=%v shield=%v",
			o.attrs.thrusterActive, o.attrs.shieldActive)
	}
	if o.attrs.shieldCapacity != 500 {
		t.Fatalf("shield capacity: want 500, got %v", o.attrs.shieldCapacity)
	}
	if o.attrs.maxSpeed <= 0 {
		t.Fatalf("power-positive skiff: want maxSpeed > 0, got %v", o.attrs.maxSpeed)
	}
}

func TestMaxSpeedIsThrustOverMass(t *testing.T) {
	o := fit(skiffBaseMass, skiffModules(), nil)
	// bare skiff mass 1000+200+150+100 = 1450; thrust 250000 → ≈172.4 u/s.
	want := 250000.0 / 1450.0
	if o.attrs.maxSpeed != want {
		t.Fatalf("max speed: want %v, got %v", want, o.attrs.maxSpeed)
	}
	if o.attrs.maxSpeed < 172 || o.attrs.maxSpeed > 173 {
		t.Fatalf("bare skiff should be ≈172 u/s, got %v", o.attrs.maxSpeed)
	}
}

func TestOverdrawCutsThruster(t *testing.T) {
	// A generator too weak (output 50) for thruster(40)+shield(30) = 70 draw.
	// Cutoff disables the thruster first; the shield (30 ≤ 50) stays powered.
	mods := []Module{
		{Mass: 200, Params: catalog.ModuleParams{PowerOutput: 50}},
		{Mass: 150, Params: catalog.ModuleParams{ShieldCapacity: 500, PowerDraw: 30}},
		{Mass: 100, Params: catalog.ModuleParams{Thrust: 250000, PowerDraw: 40}},
	}
	o := fit(skiffBaseMass, mods, nil)
	if o.attrs.thrusterActive {
		t.Fatalf("overdraw: thruster should be cut")
	}
	if !o.attrs.shieldActive {
		t.Fatalf("overdraw: shield should stay powered (30 ≤ 50)")
	}
	if o.attrs.maxSpeed != 0 {
		t.Fatalf("cut thruster: want maxSpeed 0, got %v", o.attrs.maxSpeed)
	}
	if o.attrs.powerDraw != 30 {
		t.Fatalf("after cutoff draw: want 30 (shield only), got %v", o.attrs.powerDraw)
	}
}

func TestLadenShipIsSlower(t *testing.T) {
	empty := fit(skiffBaseMass, skiffModules(), nil)
	laden := fit(skiffBaseMass, skiffModules(), []Cargo{{UnitMass: 5, Quantity: 1000}}) // +5000 mass
	if !(laden.attrs.maxSpeed < empty.attrs.maxSpeed) {
		t.Fatalf("laden ship should be slower: empty=%v laden=%v",
			empty.attrs.maxSpeed, laden.attrs.maxSpeed)
	}
}
