-- +goose Up

-- Account: credentials + status (see docs/domain-model.md, docs/database.md).
CREATE TABLE accounts (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'active',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at TIMESTAMPTZ
);

-- Character: belongs to an account; carries progression. race/intention are TBD.
CREATE TABLE characters (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    prestige   INT NOT NULL DEFAULT 0,
    faction    TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Sector: a node in the galaxy. danger/owner-faction are TBD.
CREATE TABLE sectors (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Object type catalog: registered by migration (the catalog originates in server
-- code, see docs/database.md). id is DB-only (not on the wire yet).
CREATE TABLE object_types (
    id           INT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    key          TEXT NOT NULL UNIQUE,
    name         TEXT NOT NULL,
    size_class   CHAR(1) NOT NULL, -- S/N/B/H/M (Small/Normal/Big/Huge/Mega)
    base_health  INT,
    base_shield  INT,
    base_scanner INT,
    base_jammer  INT,
    max_speed    INT
);

-- Object: the single unit of simulation. owner_character_id NULL = NPC. Position
-- is BIGINT to match the wire Vec2 (sint64). Cribbed from starraid-plain (archive).
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

-- +goose Down
DROP TABLE IF EXISTS objects;
DROP TABLE IF EXISTS object_types;
DROP TABLE IF EXISTS sectors;
DROP TABLE IF EXISTS characters;
DROP TABLE IF EXISTS accounts;
