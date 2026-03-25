CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE apps (
    id                  UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    slug                TEXT        NOT NULL,
    name                TEXT        NOT NULL,
    description         TEXT        NOT NULL DEFAULT '',
    icon                TEXT        NOT NULL DEFAULT '',
    identity_id         UUID        NOT NULL,
    service_token_hash  TEXT        NOT NULL,
    ziti_identity_id    TEXT        NOT NULL DEFAULT '',
    ziti_service_id     TEXT        NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX apps_slug_idx ON apps (slug);
CREATE UNIQUE INDEX apps_identity_id_idx ON apps (identity_id);
CREATE UNIQUE INDEX apps_service_token_hash_idx ON apps (service_token_hash);
