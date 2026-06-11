-- ============================================================
--  FinTrack — Database Schema v1
--  Migration: 001_init.sql
--  Applies automatically on startup via RunMigrations()
-- ============================================================

-- Enable uuid generation (available by default in PostgreSQL 13+)
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ─────────────────────────────────────────────
--  TABLE: users
-- ─────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS users (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    email          TEXT        UNIQUE NOT NULL,
    password_hash  TEXT        NOT NULL,
    monthly_income BIGINT      NOT NULL DEFAULT 10000000,
    wealth_goal    BIGINT      NOT NULL DEFAULT 30,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ─────────────────────────────────────────────
--  TABLE: categories
-- ─────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS categories (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT        NOT NULL,
    budget_limit BIGINT      NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, name)
);

-- ─────────────────────────────────────────────
--  TABLE: transactions
-- ─────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS transactions (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    category_name TEXT        NOT NULL DEFAULT 'uncategorized',
    amount        BIGINT      NOT NULL,
    description   TEXT        NOT NULL DEFAULT '',
    source        TEXT        NOT NULL DEFAULT 'web',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_transactions_user_id ON transactions(user_id);

-- ─────────────────────────────────────────────
--  TABLE: telegram_binds
-- ─────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS telegram_binds (
    chat_id    TEXT        PRIMARY KEY,
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    is_active  BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ─────────────────────────────────────────────
--  TABLE: verification_codes
-- ─────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS verification_codes (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    code       TEXT        UNIQUE NOT NULL,
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL
);
