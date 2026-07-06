package session

import (
	"testing"

	"github.com/xuedi/starraid-server/internal/game"
)

// TestPerceptionDelta pins the enter/update/leave diff the live loop streams as an
// object's perceived set changes over successive ticks.
func TestPerceptionDelta(t *testing.T) {
	seen := map[uint64]game.ObjectState{}

	// First sight: every perceived object is an enter.
	enter, update, leave := perceptionDelta([]game.ObjectState{
		{ID: 1, X: 0, Y: 0, TypeKey: "hauler"},
		{ID: 2, X: 5, Y: 5, TypeKey: "asteroid"},
	}, seen)
	if len(enter) != 2 || len(update) != 0 || len(leave) != 0 {
		t.Fatalf("first sight: want 2 enter/0 update/0 leave, got %d/%d/%d", len(enter), len(update), len(leave))
	}
	if len(seen) != 2 {
		t.Fatalf("seen should track 2 objects, got %d", len(seen))
	}

	// Same set, same positions: no events.
	enter, update, leave = perceptionDelta([]game.ObjectState{
		{ID: 1, X: 0, Y: 0, TypeKey: "hauler"},
		{ID: 2, X: 5, Y: 5, TypeKey: "asteroid"},
	}, seen)
	if len(enter) != 0 || len(update) != 0 || len(leave) != 0 {
		t.Fatalf("unchanged: want no events, got %d/%d/%d", len(enter), len(update), len(leave))
	}

	// One moves, the other drops out of the perceived set.
	enter, update, leave = perceptionDelta([]game.ObjectState{
		{ID: 1, X: 10, Y: 0, TypeKey: "hauler"}, // moved
	}, seen)
	if len(enter) != 0 {
		t.Fatalf("want no enter, got %+v", enter)
	}
	if len(update) != 1 || update[0].ID != 1 || update[0].X != 10 {
		t.Fatalf("want update for id 1 at X=10, got %+v", update)
	}
	if len(leave) != 1 || leave[0] != 2 {
		t.Fatalf("want leave for id 2, got %+v", leave)
	}
	if _, ok := seen[2]; ok {
		t.Fatalf("seen should have dropped id 2")
	}

	// A new object appears alongside the (now unchanged) tracked one.
	enter, update, leave = perceptionDelta([]game.ObjectState{
		{ID: 1, X: 10, Y: 0, TypeKey: "hauler"},
		{ID: 3, X: 1, Y: 1, TypeKey: "station"},
	}, seen)
	if len(enter) != 1 || enter[0].ID != 3 {
		t.Fatalf("want enter for id 3, got %+v", enter)
	}
	if len(update) != 0 || len(leave) != 0 {
		t.Fatalf("want no update/leave, got %d/%d", len(update), len(leave))
	}
}
