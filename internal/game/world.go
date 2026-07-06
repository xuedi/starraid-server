// Package game holds the authoritative in-memory world state and the tick loop.
package game

import (
	"context"
	"log/slog"
	"math"
	"sync"
	"time"
)

// defaultSpawnSpeed is the straight-line speed (world units/s) given to a freshly
// spawned object that has no fitting — the dev/offline auth path, where there is
// no DB-loaded ship to derive from. DB-loaded objects move at their derived
// maxSpeed instead (see attributes.go).
const defaultSpawnSpeed = 200.0

// perceptionRange is the placeholder distance (world units) within which one
// object can perceive another before the sensor-vs-jammer rule refines it (the
// archive's distance cull). Author-tunable placeholder like defaultSpawnSpeed —
// the starting scene spans ~11k units from origin, so this keeps most of it
// visible while still culling the far edge. Subsumed by the sensor rule once
// jammers are fitted (see canPerceive, docs/objects.md §Perception).
const perceptionRange = 10000.0

// Object is a single unit of simulation (see docs/objects.md — everything is an
// object): id + position + an optional move target, plus its per-instance fitting
// (modules + cargo) and the attributes DERIVED and cached from that fitting.
//
// Position is kept as float64 for precise per-tick integration; the wire Vec2
// (int64) is produced by rounding in snapshot.
type Object struct {
	ID uint64

	x, y      float64 // authoritative position (world units)
	tx, ty    float64 // current move target
	hasTarget bool    // whether the object is steering toward (tx, ty)

	typeKey  string   // object_class.key — carried on the wire for client sprites
	baseMass int64    // class base mass (fitting mass adds to it)
	modules  []Module // installed modules (per-instance fitting)
	cargo    []Cargo  // cargo stacks (per-instance)
	attrs    attrs    // derived + cached from baseMass+modules+cargo (recompute)
}

// ObjectState is a wire-ready snapshot of an Object: rounded position plus its
// class key (so beacons/self-assign can drive client sprite selection).
type ObjectState struct {
	ID      uint64
	X, Y    int64
	TypeKey string
}

// snapshot returns the wire-ready state of o.
func (o *Object) snapshot() ObjectState {
	return ObjectState{ID: o.ID, X: round(o.x), Y: round(o.y), TypeKey: o.typeKey}
}

func round(v float64) int64 { return int64(math.Round(v)) }

// World is the authoritative, in-memory mirror of the *active slice* of the
// universe. PostgreSQL is the single source of truth; this holds only the
// objects in active areas (see docs/server.md, docs/database.md). For now the
// store is in-memory only and ids are not durable across restarts; the
// PostgreSQL object store is a later slice.
type World struct {
	mu      sync.Mutex
	objects map[uint64]*Object
	nextID  uint64
	// Interest management: perception is distance- + sensor-gated per observer
	// (see Perceived). TODO: active-area clusters and a spatial index (the scan
	// in Perceived is O(n) per observer — fine for the single starting sector).
}

// New creates an empty world. Real startup loads the world setting and
// active-area objects from the database.
func New() *World {
	return &World{objects: make(map[uint64]*Object)}
}

// SpawnFor creates a fresh object for a connecting client and returns its
// wire-ready state. The spawn point is the galactic origin for now; faction-home
// / sector placement waits for the galaxy + DB slices. Safe for concurrent use.
func (w *World) SpawnFor() ObjectState {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.nextID++
	// No fitting to derive from (dev/offline spawn): keep it mobile at the
	// default speed so movement still works without a DB-loaded ship.
	obj := &Object{ID: w.nextID, attrs: attrs{maxSpeed: defaultSpawnSpeed}}
	w.objects[obj.ID] = obj
	return obj.snapshot()
}

// Despawn removes the object with the given id (called when its controlling
// connection drops — no persistence yet). A no-op if it is already gone.
func (w *World) Despawn(id uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.objects, id)
}

// Seed is a DB-loaded object to prime into the world at boot: id, position, class
// key + base mass, and the per-instance fitting. Load derives the object's
// attributes from it.
type Seed struct {
	ID       uint64
	X, Y     int64
	TypeKey  string
	BaseMass int64
	Modules  []Module
	Cargo    []Cargo
}

// Load inserts objects read from the database at boot, preserving their real ids
// and positions, deriving each object's attributes from its fitting, and
// advancing nextID past the highest id so later dev spawns don't collide. The DB
// is the single source of truth; this primes the in-memory active slice (see
// docs/database.md).
func (w *World) Load(seeds []Seed) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, s := range seeds {
		obj := &Object{
			ID: s.ID, x: float64(s.X), y: float64(s.Y),
			typeKey: s.TypeKey, baseMass: s.BaseMass, modules: s.Modules, cargo: s.Cargo,
		}
		obj.recompute()
		w.objects[s.ID] = obj
		if s.ID > w.nextID {
			w.nextID = s.ID
		}
	}
}

// Perceived returns wire-ready snapshots of the objects the object with the given
// id can currently perceive — the interest-managed neighbour set the session
// streams as ObjectEnter/Update/Leave (see docs/server.md §Interest management).
// An object is perceived when it is (1) within perceptionRange AND (2) not hidden
// from this observer by the sensor-vs-jammer rule (see canPerceive). Excludes the
// observer itself; returns nil if it no longer exists.
func (w *World) Perceived(id uint64) []ObjectState {
	w.mu.Lock()
	defer w.mu.Unlock()
	self, ok := w.objects[id]
	if !ok {
		return nil
	}
	const rangeSq = perceptionRange * perceptionRange
	out := make([]ObjectState, 0, len(w.objects))
	for oid, obj := range w.objects {
		if oid == id {
			continue
		}
		dx, dy := obj.x-self.x, obj.y-self.y
		if dx*dx+dy*dy > rangeSq {
			continue // out of range — distance gate (the archive's calcDist cull)
		}
		if !self.canPerceive(obj) {
			continue // hidden by a jammer stronger than our sensors — sensor gate
		}
		out = append(out, obj.snapshot())
	}
	return out
}

// canPerceive reports whether o's sensors can pick out target given target's
// jamming.
//
// PLACEHOLDER RULE (author decision — do not treat as final): the observer is
// blinded only when the target's jammer STRICTLY EXCEEDS the observer's scanner.
// So an unjammed target — and every object when nothing is fitted (0 ≤ 0) — stays
// visible, making the gate degrade safely to pure distance until jammers appear.
// The real comparison (threshold vs. ratio vs. additive, plus any size-class
// scaling) is an open decision flagged at docs/objects.md §"exact sensor-vs-jammer
// visibility". Keep the whole rule inside this one helper so swapping it later is
// a one-function change.
func (o *Object) canPerceive(target *Object) bool {
	return target.attrs.jammer <= o.attrs.scanner
}

// Get returns the wire-ready state of the object with the given id, and whether
// it exists.
func (w *World) Get(id uint64) (ObjectState, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	obj, ok := w.objects[id]
	if !ok {
		return ObjectState{}, false
	}
	return obj.snapshot(), true
}

// SetTarget tells the object to steer toward (tx, ty), halting on arrival
// (move-to-point intent). A no-op if the object no longer exists.
func (w *World) SetTarget(id uint64, tx, ty int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if obj, ok := w.objects[id]; ok {
		obj.tx, obj.ty = float64(tx), float64(ty)
		obj.hasTarget = true
	}
}

// Stop cancels the object's current move; it holds its current position. A no-op
// if the object no longer exists.
func (w *World) Stop(id uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if obj, ok := w.objects[id]; ok {
		obj.hasTarget = false
	}
}

// Count returns the number of live objects. Primarily for tests/diagnostics.
func (w *World) Count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.objects)
}

// Tick advances the simulation by one step of duration dt. Pacing is tick-based
// with an adaptive per-area rate (see docs/server.md); this stub is fixed-rate.
//
// Movement: each object with a target steers straight toward it at its DERIVED
// maxSpeed (attributes.go — thrust/mass, 0 if the thruster is cut by overdraw),
// snapping exactly to the target (and clearing it) on the tick it would reach or
// overshoot — so it halts on arrival without oscillating.
func (w *World) Tick(dt time.Duration) {
	sec := dt.Seconds()
	if sec <= 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, obj := range w.objects {
		if !obj.hasTarget {
			continue
		}
		step := obj.attrs.maxSpeed * sec
		if step <= 0 {
			continue // unpowered thruster (overdraw) — can't move
		}
		dx, dy := obj.tx-obj.x, obj.ty-obj.y
		dist := math.Hypot(dx, dy)
		if dist <= step || dist == 0 {
			obj.x, obj.y = obj.tx, obj.ty // arrived
			obj.hasTarget = false
			continue
		}
		obj.x += dx / dist * step
		obj.y += dy / dist * step
	}
	// TODO: resolve combat (size class + weapon triangle), run power/economy over
	// cached attributes, advance contracts.
}

// Run drives the tick loop until ctx is cancelled.
func (w *World) Run(ctx context.Context) {
	const interval = 100 * time.Millisecond // ~10 Hz placeholder; real rate is adaptive
	t := time.NewTicker(interval)
	defer t.Stop()
	slog.Info("world loop started", "interval", interval)

	last := time.Now()
	for {
		select {
		case <-ctx.Done():
			slog.Info("world loop stopped")
			return
		case now := <-t.C:
			w.Tick(now.Sub(last))
			last = now
		}
	}
}
