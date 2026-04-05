-- 0002_app_installation_model.sql

-- 1. Extend apps table
ALTER TABLE apps ADD COLUMN organization_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000';
ALTER TABLE apps ALTER COLUMN organization_id DROP DEFAULT;

ALTER TABLE apps ADD COLUMN visibility TEXT NOT NULL DEFAULT 'internal';
ALTER TABLE apps ADD COLUMN permissions TEXT[] NOT NULL DEFAULT '{}';

-- 2. Replace global slug uniqueness with per-org uniqueness
DROP INDEX apps_slug_idx;
CREATE UNIQUE INDEX apps_org_slug_idx ON apps (organization_id, slug);

-- 3. Create app_installations table
CREATE TABLE app_installations (
    id              UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    app_id          UUID        NOT NULL REFERENCES apps (id),
    organization_id UUID        NOT NULL,
    slug            TEXT        NOT NULL,
    configuration   JSONB       NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX app_installations_org_slug_idx ON app_installations (organization_id, slug);
CREATE INDEX app_installations_app_id_idx ON app_installations (app_id);
