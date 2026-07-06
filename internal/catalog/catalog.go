// Package catalog is the server-code source of truth for the world's STRUCTURE:
// the object classes (hull + basic attributes + slot capacity), the installable
// modules, and the cargo item kinds, defined as Go values. A Sync pushes them
// into the catalog tables (object_class / module_types / item_types) so the admin
// (and any tool) can discover what hulls/modules/items exist without importing
// this package — the DB is the contract (see docs/database.md, docs/objects.md).
//
// The catalog carries STRUCTURE and PARAMETERS only; it prescribes no loadout
// (the fitting is authored per instance by the admin seed / player) and no
// behaviour (how a generator turns fuel into power lives in code, keyed by these
// string keys — see the derivation in package game).
package catalog

// Slot is one hull slot-capacity entry: Count slots of (Kind, Size). It says what
// FITS, never what is fitted.
type Slot struct {
	Kind  string `json:"kind"`  // "internal" / "external"
	Size  string `json:"size"`  // "S"/"N"/"B"/"H"/"M"
	Count int    `json:"count"` // how many such slots the hull has
}

// Class is a code-defined object class: hull structure only — basic attributes +
// slot capacity. No loadout, no cargo (those are per-instance configuration).
type Class struct {
	Key             string
	Name            string
	Kind            string // "ship" / "structure" / "object"
	SizeClass       string // single char S/N/B/H/M
	BaseMass        int64
	BaseCargoVolume int64
	Slots           []Slot
}

// ModuleParams is the behaviour-parameter schema shared by the catalog (which
// serialises it into module_types.params) and the server's attribute derivation
// (which parses it back — see package game). A module's ROLE is read from which
// fields are set: PowerOutput>0 → generator, ShieldCapacity>0 → shield,
// Thrust>0 → thruster, Scanner>0 → sensor, Jammer>0 → jammer. A bare mount (e.g.
// a turret) sets none and contributes only its mass. Fuel burn is a later slice;
// the field is carried but unused.
type ModuleParams struct {
	PowerOutput       float64 `json:"power_output,omitempty"`
	FuelItem          string  `json:"fuel_item,omitempty"`
	FuelBurnPerEnergy float64 `json:"fuel_burn_per_energy,omitempty"`
	ShieldCapacity    float64 `json:"shield_capacity,omitempty"`
	PowerDraw         float64 `json:"power_draw,omitempty"`
	RechargeRate      float64 `json:"recharge_rate,omitempty"`
	Thrust            float64 `json:"thrust,omitempty"`
	Scanner           float64 `json:"scanner,omitempty"` // sensor strength (interest management)
	Jammer            float64 `json:"jammer,omitempty"`  // jamming strength (interest management)
}

// Module is a code-defined installable module.
type Module struct {
	Key       string
	Name      string
	SlotKind  string // "internal" / "external"
	SizeClass string
	Mass      int64
	Params    ModuleParams
}

// Item is a code-defined cargo item kind.
type Item struct {
	Key      string
	Name     string
	Category string // "fuel" / "resource" / "ammunition" / ...
	Mass     int64  // per unit
	Volume   int64  // per unit
}

// Classes is the code-defined hull roster (slot capacity only — no loadout). The
// player begins in the skiff; NPCs/structures/asteroids fill the rest. Numbers
// are placeholders (author-tunable). See the plan + docs/objects.md.
var Classes = []Class{
	{Key: "skiff", Name: "Skiff", Kind: "ship", SizeClass: "S", BaseMass: 1000, BaseCargoVolume: 50,
		Slots: []Slot{{Kind: "internal", Size: "N", Count: 3}, {Kind: "external", Size: "N", Count: 1}}},
	{Key: "corvette", Name: "Corvette", Kind: "ship", SizeClass: "N", BaseMass: 4000, BaseCargoVolume: 200,
		Slots: []Slot{{Kind: "internal", Size: "N", Count: 4}, {Kind: "external", Size: "N", Count: 2}}},
	{Key: "hauler", Name: "Hauler", Kind: "ship", SizeClass: "B", BaseMass: 14000, BaseCargoVolume: 5000,
		Slots: []Slot{{Kind: "internal", Size: "B", Count: 4}, {Kind: "external", Size: "N", Count: 1}}},
	{Key: "cruiser", Name: "Cruiser", Kind: "ship", SizeClass: "H", BaseMass: 40000, BaseCargoVolume: 1000,
		Slots: []Slot{{Kind: "internal", Size: "H", Count: 6}, {Kind: "external", Size: "N", Count: 4}}},
	{Key: "station", Name: "Station", Kind: "structure", SizeClass: "M", BaseMass: 200000, BaseCargoVolume: 20000,
		Slots: []Slot{{Kind: "internal", Size: "M", Count: 10}, {Kind: "external", Size: "B", Count: 4}}},
	{Key: "asteroid", Name: "Asteroid", Kind: "object", SizeClass: "B", BaseMass: 50000, BaseCargoVolume: 8000,
		Slots: nil},
}

// Modules is the code-defined module roster. params are placeholders (power.md).
var Modules = []Module{
	{Key: "gen_hydrogen_basic", Name: "Basic Hydrogen Generator", SlotKind: "internal", SizeClass: "N", Mass: 200,
		Params: ModuleParams{PowerOutput: 100, FuelItem: "hydrogen", FuelBurnPerEnergy: 0.01}},
	{Key: "shield_basic", Name: "Basic Shield Generator", SlotKind: "internal", SizeClass: "N", Mass: 150,
		Params: ModuleParams{ShieldCapacity: 500, PowerDraw: 30, RechargeRate: 20}},
	{Key: "thruster_ion", Name: "Ion Thruster", SlotKind: "internal", SizeClass: "N", Mass: 100,
		Params: ModuleParams{Thrust: 250000, PowerDraw: 40}},
	{Key: "sensor_basic", Name: "Basic Sensor Array", SlotKind: "external", SizeClass: "N", Mass: 80,
		Params: ModuleParams{Scanner: 100, PowerDraw: 15}},
	{Key: "jammer_basic", Name: "Basic ECM Jammer", SlotKind: "external", SizeClass: "N", Mass: 120,
		Params: ModuleParams{Jammer: 100, PowerDraw: 25}},
}

// Items is the code-defined cargo item roster.
var Items = []Item{
	{Key: "hydrogen", Name: "Hydrogen", Category: "fuel", Mass: 1, Volume: 1},
	{Key: "iron_ore", Name: "Iron Ore", Category: "resource", Mass: 5, Volume: 2},
	{Key: "ammunition", Name: "Ammunition", Category: "ammunition", Mass: 2, Volume: 1},
	{Key: "ice", Name: "Ice", Category: "resource", Mass: 3, Volume: 3},
}
