-- password_hash keeps its '' default (rather than becoming NOT NULL with
-- no default) so every existing call site that creates a user without a
-- password - test fixtures throughout the suite - keeps working; a user
-- with an empty hash simply can never satisfy a login password check.
ALTER TABLE users ADD COLUMN password_hash TEXT NOT NULL DEFAULT '';
