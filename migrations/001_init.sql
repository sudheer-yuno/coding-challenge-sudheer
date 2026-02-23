-- Batch Payout Processing Engine Schema

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Payout batches table
CREATE TABLE IF NOT EXISTS payout_batches (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    status          VARCHAR(20) NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending', 'in_progress', 'completed', 'failed', 'partially_completed')),
    total_count     INT NOT NULL DEFAULT 0,
    completed_count INT NOT NULL DEFAULT 0,
    failed_count    INT NOT NULL DEFAULT 0,
    pending_count   INT NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Individual payouts table
CREATE TABLE IF NOT EXISTS payouts (
    id               UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    batch_id         UUID NOT NULL REFERENCES payout_batches(id),
    idempotency_key  VARCHAR(255) NOT NULL UNIQUE,
    vendor_id        VARCHAR(100) NOT NULL,
    vendor_name      VARCHAR(255),
    amount           DECIMAL(15,2) NOT NULL,
    currency         VARCHAR(3) NOT NULL DEFAULT 'USD',
    bank_account     VARCHAR(100),
    bank_name        VARCHAR(255),
    transaction_ids  TEXT[],
    status           VARCHAR(20) NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending', 'processing', 'completed', 'failed')),
    failure_reason   TEXT,
    attempt_count    INT NOT NULL DEFAULT 0,
    max_retries      INT NOT NULL DEFAULT 3,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    attempted_at     TIMESTAMPTZ,
    completed_at     TIMESTAMPTZ,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes for efficient querying
CREATE INDEX idx_payouts_batch_id ON payouts(batch_id);
CREATE INDEX idx_payouts_batch_status ON payouts(batch_id, status);
CREATE INDEX idx_payouts_vendor_id ON payouts(vendor_id);
CREATE INDEX idx_payouts_idempotency ON payouts(idempotency_key);

-- Payout attempt log for audit trail
CREATE TABLE IF NOT EXISTS payout_attempts (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    payout_id   UUID NOT NULL REFERENCES payouts(id),
    attempt_num INT NOT NULL,
    status      VARCHAR(20) NOT NULL,
    error       TEXT,
    started_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ
);

CREATE INDEX idx_attempts_payout_id ON payout_attempts(payout_id);
