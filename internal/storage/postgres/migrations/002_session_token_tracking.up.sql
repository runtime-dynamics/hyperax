-- Migration 002: Add per-session token tracking columns.
ALTER TABLE sessions
    ADD COLUMN prompt_tokens INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN completion_tokens INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN total_tokens INTEGER NOT NULL DEFAULT 0;
