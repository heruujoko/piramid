CREATE TABLE IF NOT EXISTS goals (
    id TEXT PRIMARY KEY,
    text TEXT NOT NULL,
    project_path TEXT NOT NULL,
    status TEXT NOT NULL,
    goal_path TEXT NOT NULL,
    plan_path TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS tasks (
    id TEXT PRIMARY KEY,
    goal_id TEXT NOT NULL REFERENCES goals(id),
    parent_task_id TEXT REFERENCES tasks(id),
    title TEXT NOT NULL,
    goal_text TEXT NOT NULL,
    project_path TEXT NOT NULL,
    task_path TEXT NOT NULL,
    task_hash TEXT NOT NULL,
    task_json TEXT NOT NULL,
    status TEXT NOT NULL,
    model TEXT NOT NULL,
    max_attempts INTEGER NOT NULL CHECK (max_attempts > 0),
    timeout_seconds INTEGER NOT NULL CHECK (timeout_seconds > 0),
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    next_run_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS task_dependencies (
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    depends_on_task_id TEXT NOT NULL REFERENCES tasks(id),
    PRIMARY KEY (task_id, depends_on_task_id),
    CHECK (task_id <> depends_on_task_id)
);

CREATE TABLE IF NOT EXISTS attempts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id TEXT NOT NULL REFERENCES tasks(id),
    attempt_number INTEGER NOT NULL CHECK (attempt_number > 0),
    worker_id TEXT NOT NULL,
    status TEXT NOT NULL,
    runtime TEXT NOT NULL,
    model TEXT NOT NULL,
    prompt_path TEXT NOT NULL,
    prompt_hash TEXT NOT NULL,
    stdout_path TEXT NOT NULL,
    stderr_path TEXT NOT NULL,
    process_id INTEGER,
    exit_code INTEGER,
    started_at TEXT NOT NULL,
    finished_at TEXT,
    failure_class TEXT,
    UNIQUE (task_id, attempt_number)
);

CREATE TABLE IF NOT EXISTS artifacts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    attempt_id INTEGER NOT NULL REFERENCES attempts(id) ON DELETE CASCADE,
    relative_path TEXT NOT NULL,
    absolute_path TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
    UNIQUE (attempt_id, relative_path)
);

CREATE TABLE IF NOT EXISTS verifications (
    attempt_id INTEGER PRIMARY KEY REFERENCES attempts(id) ON DELETE CASCADE,
    status TEXT NOT NULL,
    report_path TEXT NOT NULL,
    reason_summary TEXT NOT NULL,
    retry_prompt TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS workspace_leases (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_path TEXT NOT NULL,
    mode TEXT NOT NULL CHECK (mode IN ('READ', 'WRITE')),
    holder_type TEXT NOT NULL,
    holder_id TEXT NOT NULL,
    acquired_at TEXT NOT NULL,
    UNIQUE (project_path, holder_type, holder_id)
);

CREATE TABLE IF NOT EXISTS events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_tasks_runnable
    ON tasks(status, next_run_at, created_at, id);
CREATE INDEX IF NOT EXISTS idx_task_dependencies_task
    ON task_dependencies(task_id);
CREATE INDEX IF NOT EXISTS idx_task_dependencies_dependency
    ON task_dependencies(depends_on_task_id);
CREATE INDEX IF NOT EXISTS idx_attempts_task
    ON attempts(task_id, attempt_number);
CREATE INDEX IF NOT EXISTS idx_workspace_leases_project_mode
    ON workspace_leases(project_path, mode);
CREATE INDEX IF NOT EXISTS idx_events_entity
    ON events(entity_type, entity_id, id);
CREATE INDEX IF NOT EXISTS idx_events_order
    ON events(id);
