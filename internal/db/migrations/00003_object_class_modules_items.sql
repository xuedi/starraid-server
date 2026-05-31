-- +goose Up
-- Reshape the catalog + instance schema for the template→instance, module-derived
-- model (see docs/database.md, docs/objects.md). Catalog tables (object_class,
-- module_types, item_types) carry STRUCTURE — synced from the server's catalog
-- package at startup (code is the source of truth). Instance tables (objects,
-- object_modules, object_items) carry CONFIGURATION — authored by the admin seed
-- (and later the player). This replaces Phase-3's object_types catalog.

DROP TABLE IF EXISTS objects;
DROP TABLE IF EXISTS object_types;

-- Object class catalog: hull STRUCTURE only — basic attributes + slot capacity
-- (how many slots, of what kind/size). No loadout: the fitting is authored per
-- instance. slots is [{kind,size,count}] — capacity, not contents.
CREATE TABLE object_class (
    id                INT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    key               TEXT NOT NULL UNIQUE,
    name              TEXT NOT NULL,
    kind              TEXT NOT NULL,           -- ship / structure / object
    size_class        CHAR(1) NOT NULL,        -- S/N/B/H/M (Small/Normal/Big/Huge/Mega)
    base_mass         BIGINT NOT NULL,
    base_cargo_volume BIGINT NOT NULL,
    slots             JSONB NOT NULL DEFAULT '[]'
);

-- Module catalog: installable modules (generators, shields, thrusters, turret
-- mounts). params carries behaviour PARAMETERS keyed by the catalog string key;
-- the behaviour itself lives in server code.
CREATE TABLE module_types (
    id         INT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    key        TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    slot_kind  TEXT NOT NULL,                  -- internal / external
    size_class CHAR(1) NOT NULL,
    mass       BIGINT NOT NULL,
    params     JSONB NOT NULL DEFAULT '{}'
);

-- Item catalog: cargo item kinds (fuel / resource / ammunition / …).
CREATE TABLE item_types (
    id       INT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    key      TEXT NOT NULL UNIQUE,
    name     TEXT NOT NULL,
    category TEXT NOT NULL,
    mass     BIGINT NOT NULL,
    volume   BIGINT NOT NULL
);

-- Object: a single unit of simulation, an instance of an object_class. The class
-- gives structure; the fitting (object_modules/object_items) is authored per
-- instance. owner_character_id NULL = NPC/structure/asteroid. health/shield are
-- nullable: the server stamps them from derived attributes (combat is later).
CREATE TABLE objects (
    id                 BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    object_class_id    INT NOT NULL REFERENCES object_class(id),
    owner_character_id BIGINT REFERENCES characters(id) ON DELETE SET NULL,
    sector_id          BIGINT NOT NULL REFERENCES sectors(id),
    name               TEXT,
    status             TEXT NOT NULL DEFAULT 'active',
    x                  BIGINT NOT NULL DEFAULT 0,
    y                  BIGINT NOT NULL DEFAULT 0,
    health             INT,
    shield             INT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX objects_sector_id_idx ON objects (sector_id);

-- Per-instance fitting: modules referenced into the object's slots. One row
-- places one module_types into one (slot_kind, slot_index) slot, honouring the
-- class's slot capacity. Authored by the seed/player — no class default.
CREATE TABLE object_modules (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    object_id      BIGINT NOT NULL REFERENCES objects(id) ON DELETE CASCADE,
    module_type_id INT NOT NULL REFERENCES module_types(id),
    slot_kind      TEXT NOT NULL,
    slot_index     INT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'active',
    quality        INT NOT NULL DEFAULT 100
);
CREATE INDEX object_modules_object_id_idx ON object_modules (object_id);

-- Per-instance cargo: items in the object's base hold (module_id NULL) or a
-- module's bay. quantity is the stack size.
CREATE TABLE object_items (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    object_id    BIGINT NOT NULL REFERENCES objects(id) ON DELETE CASCADE,
    module_id    BIGINT REFERENCES object_modules(id) ON DELETE SET NULL,
    item_type_id INT NOT NULL REFERENCES item_types(id),
    quantity     BIGINT NOT NULL
);
CREATE INDEX object_items_object_id_idx ON object_items (object_id);

-- +goose Down
-- Restore the Phase-3 shape (object_types + objects with baked combat stats). The
-- 'spaceship' seed row is not restored — the synced catalog replaced it.
DROP TABLE IF EXISTS object_items;
DROP TABLE IF EXISTS object_modules;
DROP TABLE IF EXISTS objects;
DROP TABLE IF EXISTS item_types;
DROP TABLE IF EXISTS module_types;
DROP TABLE IF EXISTS object_class;

CREATE TABLE object_types (
    id           INT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    key          TEXT NOT NULL UNIQUE,
    name         TEXT NOT NULL,
    size_class   CHAR(1) NOT NULL,
    base_health  INT,
    base_shield  INT,
    base_scanner INT,
    base_jammer  INT,
    max_speed    INT
);

CREATE TABLE objects (
    id                 BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    type_id            INT NOT NULL REFERENCES object_types(id),
    owner_character_id BIGINT REFERENCES characters(id) ON DELETE SET NULL,
    sector_id          BIGINT NOT NULL REFERENCES sectors(id),
    name               TEXT,
    status             TEXT NOT NULL DEFAULT 'active',
    x                  BIGINT NOT NULL DEFAULT 0,
    y                  BIGINT NOT NULL DEFAULT 0,
    health             INT,
    shield             INT,
    scanner            INT,
    jammer             INT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX objects_sector_id_idx ON objects (sector_id);
