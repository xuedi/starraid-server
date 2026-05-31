-- +goose Up
-- Register the one catalog type Phase 3 needs. Stats are placeholder (the archive
-- hard-codes none; real values are module-derived, combat is the Phase-5 TBD).
-- base_scanner 50000 echoes starraid-plain's calcDist; max_speed 200 matches the
-- server's current placeholder maxSpeed.
INSERT INTO object_types (key, name, size_class, base_health, base_shield, base_scanner, base_jammer, max_speed)
VALUES ('spaceship', 'Spaceship', 'N', 1000, 500, 50000, 0, 200)
ON CONFLICT (key) DO NOTHING;

-- +goose Down
DELETE FROM object_types WHERE key = 'spaceship';
