ALTER TABLE attempts ADD COLUMN verifier_stdout_path TEXT NOT NULL DEFAULT '';
ALTER TABLE attempts ADD COLUMN verifier_stderr_path TEXT NOT NULL DEFAULT '';
