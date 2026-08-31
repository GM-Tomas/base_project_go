CREATE TABLE IF NOT EXISTS platforms (
    user_id    UUID        NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    name       TEXT        NOT NULL,
    type       TEXT        NOT NULL DEFAULT 'Other',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, name),
    CONSTRAINT platforms_name_len CHECK (char_length(btrim(name)) BETWEEN 1 AND 120),
    CONSTRAINT platforms_type_len CHECK (char_length(btrim(type)) BETWEEN 1 AND 40)
);

-- Case-insensitive uniqueness: 'Binance' and 'binance' can't coexist for the same user.
CREATE UNIQUE INDEX IF NOT EXISTS platforms_user_lower_name_uk ON platforms (user_id, lower(name));

CREATE TABLE IF NOT EXISTS holdings (
    id            UUID          PRIMARY KEY,
    user_id       UUID          NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    name          TEXT          NOT NULL,
    asset_class   TEXT          NOT NULL,
    platform_name TEXT          NOT NULL,
    value_usd     NUMERIC(20,2) NOT NULL,
    created_at    TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT holdings_value_non_negative CHECK (value_usd >= 0),
    CONSTRAINT holdings_name_len  CHECK (char_length(btrim(name))        BETWEEN 1 AND 120),
    CONSTRAINT holdings_class_len CHECK (char_length(btrim(asset_class)) BETWEEN 1 AND 60),
    -- A holding can only reference a platform belonging to the SAME user.
    CONSTRAINT holdings_platform_fk FOREIGN KEY (user_id, platform_name)
        REFERENCES platforms (user_id, name) ON UPDATE CASCADE ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS holdings_user_created_idx  ON holdings (user_id, created_at);
CREATE INDEX IF NOT EXISTS holdings_user_class_idx    ON holdings (user_id, asset_class);
CREATE INDEX IF NOT EXISTS holdings_user_platform_idx ON holdings (user_id, platform_name);

CREATE TABLE IF NOT EXISTS net_worth_snapshots (
    id              UUID          PRIMARY KEY,
    user_id         UUID          NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    captured_at     TIMESTAMPTZ   NOT NULL DEFAULT now(),
    total_value_usd NUMERIC(20,2) NOT NULL,
    CONSTRAINT snapshots_value_non_negative CHECK (total_value_usd >= 0),
    -- One snapshot per second per user — protects against a double-click (CA-06.4).
    CONSTRAINT snapshots_user_instant_uk UNIQUE (user_id, captured_at)
);

CREATE INDEX IF NOT EXISTS snapshots_user_captured_idx ON net_worth_snapshots (user_id, captured_at);

ALTER TABLE platforms           ENABLE ROW LEVEL SECURITY;
ALTER TABLE holdings            ENABLE ROW LEVEL SECURITY;
ALTER TABLE net_worth_snapshots ENABLE ROW LEVEL SECURITY;

DO $$ 
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname = 'own platforms' AND tablename = 'platforms') THEN
        CREATE POLICY "own platforms" ON platforms FOR ALL USING (auth.uid() = user_id) WITH CHECK (auth.uid() = user_id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname = 'own holdings' AND tablename = 'holdings') THEN
        CREATE POLICY "own holdings" ON holdings FOR ALL USING (auth.uid() = user_id) WITH CHECK (auth.uid() = user_id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname = 'own snapshots' AND tablename = 'net_worth_snapshots') THEN
        CREATE POLICY "own snapshots" ON net_worth_snapshots FOR ALL USING (auth.uid() = user_id) WITH CHECK (auth.uid() = user_id);
    END IF;
END $$;
