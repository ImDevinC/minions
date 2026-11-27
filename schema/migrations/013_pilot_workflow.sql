CREATE TABLE IF NOT EXISTS minion_pilot_runs (
    minion_id uuid PRIMARY KEY REFERENCES minions(id) ON DELETE CASCADE,
    step text NOT NULL DEFAULT 'implement' CHECK (step IN ('implement','verify','publish','ci','done')),
    patch text NOT NULL DEFAULT '',
    base_sha text NOT NULL DEFAULT '',
    head_sha text NOT NULL DEFAULT '',
    tree_sha text NOT NULL DEFAULT '',
    evidence text NOT NULL DEFAULT '',
    clarification_round smallint NOT NULL DEFAULT 1 CHECK (clarification_round IN (1,2)),
    changed_at timestamptz NOT NULL DEFAULT now(),
    next_poll timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE minion_pilot_runs ADD COLUMN IF NOT EXISTS cleaned boolean NOT NULL DEFAULT false;
CREATE INDEX IF NOT EXISTS idx_minion_pilot_ready ON minion_pilot_runs(next_poll) WHERE step <> 'done';
