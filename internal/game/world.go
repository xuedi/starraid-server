// Package game holds the authoritative in-memory world state and the tick loop.
package game

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Object is a single unit of simulation (see docs/objects.md — everything is an
// object). This slice models only id + position; type, status, owner, installed
// modules and their derived attributes follow in later slices.
type Object struct {
	ID   uint64
	X, Y int64
}

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

// SpawnFor creates a fresh object for a connecting client and returns a snapshot
// of it. The spawn point is the galactic origin for now; faction-home / sector
// placement waits for the galaxy + DB slices. Safe for concurrent use.
func (w *World) SpawnFor() Object {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.nextID++
	obj := &Object{ID: w.nextID, X: 0, Y: 0}
	w.objects[obj.ID] = obj
	return *obj
}

// Despawn removes the object with the given id (called when its controlling
// connection drops — no persistence yet). A no-op if it is already gone.
func (w *World) Despawn(id uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.objects, id)
}

// Get returns a snapshot of the object with the given id, and whether it exists.
func (w *World) Get(id uint64) (Object, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	obj, ok := w.objects[id]
	if !ok {
		return Object{}, false
	}
	return *obj, true
}

// Count returns the number of live objects. Primarily for tests/diagnostics.
func (w *World) Count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.objects)
}

// Tick advances the simulation by one step of duration dt. Pacing is tick-based
// with an adaptive per-area rate (see docs/server.md); this stub is fixed-rate.
func (w *World) Tick(dt time.Duration) {
	// TODO: integrate movement, resolve combat (size class + weapon triangle),
	// run power/economy over cached attributes, advance contracts.
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
