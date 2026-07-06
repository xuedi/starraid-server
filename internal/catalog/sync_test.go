package catalog

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// fakeExecer records the SQL of every Exec so the test can assert which catalog
// tables Sync upserts into, and how many rows, without a live database.
type fakeExecer struct {
	classes, modules, items int
	err                     error
}

func (f *fakeExecer) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	switch {
	case strings.Contains(sql, "INTO object_class"):
		f.classes++
	case strings.Contains(sql, "INTO module_types"):
		f.modules++
	case strings.Contains(sql, "INTO item_types"):
		f.items++
	}
	return pgconn.CommandTag{}, f.err
}

func TestSyncUpsertsWholeRoster(t *testing.T) {
	f := &fakeExecer{}
	if err := Sync(context.Background(), f); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if f.classes != len(Classes) || f.classes != 6 {
		t.Fatalf("classes upserted: want 6, got %d (roster %d)", f.classes, len(Classes))
	}
	if f.modules != len(Modules) || f.modules != 5 {
		t.Fatalf("modules upserted: want 5, got %d (roster %d)", f.modules, len(Modules))
	}
	if f.items != len(Items) || f.items != 4 {
		t.Fatalf("items upserted: want 4, got %d (roster %d)", f.items, len(Items))
	}
}

// TestRosterKeysUnique guards against a copy-paste duplicate key in the rosters
// (the catalog tables enforce UNIQUE(key); fail fast in a unit test instead).
func TestRosterKeysUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range Classes {
		if seen["class:"+c.Key] {
			t.Fatalf("duplicate class key %q", c.Key)
		}
		seen["class:"+c.Key] = true
	}
	for _, m := range Modules {
		if seen["module:"+m.Key] {
			t.Fatalf("duplicate module key %q", m.Key)
		}
		seen["module:"+m.Key] = true
	}
	for _, it := range Items {
		if seen["item:"+it.Key] {
			t.Fatalf("duplicate item key %q", it.Key)
		}
		seen["item:"+it.Key] = true
	}
}
