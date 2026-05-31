package game

import (
	"sync"
	"testing"
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

// TestConcurrentAccess exercises the mutex under -race.
func TestConcurrentAccess(t *testing.T) {
	w := New()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			obj := w.SpawnFor()
			_, _ = w.Get(obj.ID)
			_ = w.Count()
			w.Despawn(obj.ID)
		}()
	}
	wg.Wait()
	if w.Count() != 0 {
		t.Fatalf("want 0 objects after concurrent spawn/despawn, got %d", w.Count())
	}
}
