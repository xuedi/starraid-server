package stats

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestSnapshotCountsSessionsAndObjects(t *testing.T) {
	objs := 3
	r := New(func() int { return objs }, 10.0)

	if s := r.Snapshot(); s.Sessions != 0 || s.Objects != 3 || s.TickHz != 10.0 {
		t.Fatalf("initial snapshot = %+v", s)
	}

	r.SessionStart()
	r.SessionStart()
	if s := r.Snapshot(); s.Sessions != 2 {
		t.Fatalf("after 2 starts: sessions = %d, want 2", s.Sessions)
	}
	r.SessionEnd()
	objs = 5
	if s := r.Snapshot(); s.Sessions != 1 || s.Objects != 5 {
		t.Fatalf("after end + object change: %+v", s)
	}
	if s := r.Snapshot(); s.UptimeS < 0 {
		t.Fatalf("uptime should be non-negative, got %v", s.UptimeS)
	}
}

func TestHandlerServesJSON(t *testing.T) {
	r := New(func() int { return 7 }, 10.0)
	r.SessionStart()

	rec := httptest.NewRecorder()
	r.Handler()(rec, httptest.NewRequest("GET", "/stats", nil))

	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q", ct)
	}
	var got Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Objects != 7 || got.Sessions != 1 {
		t.Fatalf("decoded snapshot = %+v", got)
	}
}
