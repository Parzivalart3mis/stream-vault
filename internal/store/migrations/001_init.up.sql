CREATE TABLE IF NOT EXISTS creators (
  id            UUID PRIMARY KEY,
  display_name  TEXT NOT NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS events (
  id            UUID PRIMARY KEY,
  creator_id    UUID NOT NULL REFERENCES creators(id) ON DELETE CASCADE,
  viewer_id     TEXT NOT NULL,
  event_type    TEXT NOT NULL CHECK (event_type IN ('subscription','gift','tip')),
  tier          SMALLINT,
  quantity      INT  NOT NULL DEFAULT 1,
  amount_cents  BIGINT NOT NULL CHECK (amount_cents > 0),
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_events_creator_time ON events (creator_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_events_type_time    ON events (event_type, created_at DESC);
