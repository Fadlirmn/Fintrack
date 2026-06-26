-- ============================================================
--  FinTrack — Migration 003
--  Adds display name to users table for profile editing
-- ============================================================

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT '';
