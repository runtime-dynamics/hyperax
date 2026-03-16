CREATE TABLE IF NOT EXISTS test_posts (
    id      INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES test_users(id),
    title   TEXT NOT NULL,
    body    TEXT NOT NULL DEFAULT ''
);
