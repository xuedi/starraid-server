// Package game holds the authoritative in-memory world state and the tick loop.
package game

import (
	"context"
	"log/slog"
	"math"
	"sync"
	"time"
)

// maxSpeed is the placeholder straight-line speed (world units per second) every
// object moves at while it has a move target. Real speed is derived from the
// object's thruster modules and mass (see docs/objects.md); this single constant
// stands in until that model lands.
const maxSpeed = 200.0 // TODO: module-derived

// Object is a single unit of simulation (see docs/objects.md — everything is an
// object). This slice models id + position + an optional move target; type,
// status, owner, installed modules and their derived attributes follow later.
//
// Position is kept as float64 for precise per-tick integration; the wire Vec2
// (int64) is produced by rounding in Snapshot.
type Object struct {
	ID uint64

	x, y      float64 // authoritative position (world units)
	tx, ty    float64 // current move target
	hasTarget bool    // whether the object is steering toward (tx, ty)
}

// ObjectState is a wire-ready snapshot of an Object's position (rounded to the
// int64 universe grid).
type ObjectState struct {
	ID   uint64
	X, Y int64
}

// snapshot returns the wire-ready state of o.
func (o *Object) snapshot() ObjectState {
	return ObjectState{ID: o.ID, X: round(o.x), Y: round(o.y)}
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
	// TODO: active-area clusters, interest management, cached per-object
	// attributes (recomputed on module-config change).
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
	obj := &Object{ID: w.nextID}
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
// Movement: each object with a target steers straight toward it at maxSpeed,
// snapping exactly to the target (and clearing it) on the tick it would reach or
// overshoot — so it halts on arrival without oscillating.
func (w *World) Tick(dt time.Duration) {
	step := maxSpeed * dt.Seconds()
	if step <= 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, obj := range w.objects {
		if !obj.hasTarget {
			continue
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
