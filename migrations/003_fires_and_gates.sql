CREATE TABLE IF NOT EXISTS fires (
    id TEXT PRIMARY KEY,
    loop_id TEXT NOT NULL,
    goal_id TEXT REFERENCES goals(id),
    status TEXT NOT NULL,
    scheduled_at TEXT NOT NULL,
    started_at TEXT,
    finished_at TEXT,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS gates (
    id TEXT PRIMARY KEY,
    fire_id TEXT NOT NULL REFERENCES fires(id),
    goal_id TEXT REFERENCES goals(id),
    task_id TEXT REFERENCES tasks(id),
    attempt_id INTEGER REFERENCES attempts(id),
    status TEXT NOT NULL,
    context_path TEXT NOT NULL,
    context_json TEXT NOT NULL DEFAULT '{}',
    context_body TEXT NOT NULL DEFAULT '',
    decision TEXT,
    decision_note TEXT NOT NULL DEFAULT '',
    opened_at TEXT NOT NULL,
    resolved_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_fires_loop_scheduled ON fires(loop_id, scheduled_at);
CREATE INDEX IF NOT EXISTS idx_fires_status ON fires(status);
CREATE INDEX IF NOT EXISTS idx_gates_status ON gates(status);
CREATE INDEX IF NOT EXISTS idx_gates_fire ON gates(fire_id);
CREATE INDEX IF NOT EXISTS idx_gates_goal ON gates(goal_id);
