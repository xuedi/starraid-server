package game

import (
	"sync"
	"testing"
	"time"
)

func TestSpawnGetDespawn(t *testing.T) {
	w := New()
	if w.Count() != 0 {
		t.Fatalf("new world: want 0 objects, got %d", w.Count())
	}

	obj := w.SpawnFor()
	if obj.ID == 0 {
		t.Fatalf("SpawnFor: want a non-zero id")
	}
	if w.Count() != 1 {
		t.Fatalf("after spawn: want 1 object, got %d", w.Count())
	}

	got, ok := w.Get(obj.ID)
	if !ok {
		t.Fatalf("Get(%d): not found after spawn", obj.ID)
	}
	if got != obj {
		t.Fatalf("Get returned %+v, want %+v", got, obj)
	}

	w.Despawn(obj.ID)
	if _, ok := w.Get(obj.ID); ok {
		t.Fatalf("Get(%d): still present after despawn", obj.ID)
	}
	if w.Count() != 0 {
		t.Fatalf("after despawn: want 0 objects, got %d", w.Count())
	}
	w.Despawn(obj.ID) // idempotent
}

func TestSpawnIDsUnique(t *testing.T) {
	w := New()
	seen := make(map[uint64]bool)
	for i := 0; i < 100; i++ {
		obj := w.SpawnFor()
		if seen[obj.ID] {
			t.Fatalf("duplicate id %d", obj.ID)
		}
		seen[obj.ID] = true
	}
	if w.Count() != 100 {
		t.Fatalf("want 100 objects, got %d", w.Count())
	}
}

func TestMoveTowardAndArrive(t *testing.T) {
	w := New()
	obj := w.SpawnFor() // at origin
	// maxSpeed is 200 units/s; a 1s tick advances 200 units toward the target.
	w.SetTarget(obj.ID, 1000, 0)

	after, _ := tickGet(w, obj.ID, time.Second)
	if after.X != 200 || after.Y != 0 {
		t.Fatalf("after 1 tick: want (200,0), got (%d,%d)", after.X, after.Y)
	}

	// Five 1s ticks total cover the 1000-unit distance; the object then halts
	// exactly on the target (no overshoot) and stays put on further ticks.
	for i := 0; i < 4; i++ {
		w.Tick(time.Second)
	}
	arrived, _ := w.Get(obj.ID)
	if arrived.X != 1000 || arrived.Y != 0 {
		t.Fatalf("after arrival: want (1000,0), got (%d,%d)", arrived.X, arrived.Y)
	}
	again, _ := tickGet(w, obj.ID, time.Second)
	if again != arrived {
		t.Fatalf("moved past target: was %+v, now %+v", arrived, again)
	}
}

func TestStopHalts(t *testing.T) {
	w := New()
	obj := w.SpawnFor()
	w.SetTarget(obj.ID, 0, 10000)

	moved, _ := tickGet(w, obj.ID, time.Second)
	if moved.Y != 200 {
		t.Fatalf("want Y=200 after one tick, got %d", moved.Y)
	}
	w.Stop(obj.ID)
	halted, _ := tickGet(w, obj.ID, time.Second)
	if halted != moved {
		t.Fatalf("Stop did not halt: was %+v, now %+v", moved, halted)
	}
}

// tickGet advances the world by dt and returns the object's resulting state.
func tickGet(w *World, id uint64, dt time.Duration) (ObjectState, bool) {
	w.Tick(dt)
	return w.Get(id)
}

// TestConcurrentAccess exercises the mutex under -race.
func TestConcurrentAccess(t *testing.T) {
	w := New()

	// A concurrent ticker mutates positions while sessions spawn, steer, read,
	// and despawn — the data-race surface this slice introduces.
	stop := make(chan struct{})
	var ticker sync.WaitGroup
	ticker.Add(1)
	go func() {
		defer ticker.Done()
		for {
			select {
			case <-stop:
				return
			default:
				w.Tick(10 * time.Millisecond)
			}
		}
	}()

	var workers sync.WaitGroup
	for i := 0; i < 50; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			obj := w.SpawnFor()
			w.SetTarget(obj.ID, 1000, 1000)
			_, _ = w.Get(obj.ID)
			_ = w.Count()
			w.Stop(obj.ID)
			w.Despawn(obj.ID)
		}()
	}
	workers.Wait()
	close(stop)
	ticker.Wait()

	if w.Count() != 0 {
		t.Fatalf("want 0 objects after concurrent spawn/despawn, got %d", w.Count())
	}
}
