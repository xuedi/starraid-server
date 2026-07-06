// Package stats exposes a small read-only telemetry surface for the running
// server — live counts that control tools (stackctl, the admin console) poll over
// HTTP. Deliberately cheap: atomic counters plus a world accessor, marshalled to
// JSON on demand. See docs/server.md; consumed by stackctl's telemetry pane.
package stats

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"
)

// Registry holds the server's live telemetry counters.
type Registry struct {
	sessions  atomic.Int64
	objects   func() int // world object count (live active slice)
	tickHz    float64
	startedAt time.Time
}

// New builds a registry. objects reports the live world object count (e.g.
// game.World.Count); tickHz is the simulation rate (placeholder-fixed for now).
func New(objects func() int, tickHz float64) *Registry {
	return &Registry{objects: objects, tickHz: tickHz, startedAt: time.Now()}
}

// SessionStart/SessionEnd track authenticated live sessions (bots included — they
// connect as ordinary clients, so this is "connections", not "humans"; a
// npc-vs-client split waits for a functional dispatcher, see docs/npc.md).
func (r *Registry) SessionStart() { r.sessions.Add(1) }
func (r *Registry) SessionEnd()   { r.sessions.Add(-1) }

// Snapshot is the JSON shape served at /stats.
type Snapshot struct {
	Objects  int     `json:"objects"`
	Sessions int64   `json:"sessions"`
	TickHz   float64 `json:"tick_hz"`
	UptimeS  float64 `json:"uptime_s"`
}

// Snapshot reads the current counts.
func (r *Registry) Snapshot() Snapshot {
	objs := 0
	if r.objects != nil {
		objs = r.objects()
	}
	return Snapshot{
		Objects:  objs,
		Sessions: r.sessions.Load(),
		TickHz:   r.tickHz,
		UptimeS:  time.Since(r.startedAt).Seconds(),
	}
}

// Handler serves the snapshot as JSON.
func (r *Registry) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(r.Snapshot())
	}
}
