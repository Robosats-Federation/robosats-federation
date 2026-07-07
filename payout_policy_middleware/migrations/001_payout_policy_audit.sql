CREATE TABLE IF NOT EXISTS payout_policy_attempts (
  id BIGSERIAL PRIMARY KEY,
  request_id TEXT UNIQUE,
  order_id BIGINT NOT NULL,
  payment_hash TEXT NOT NULL,
  amount_sats BIGINT NOT NULL,
  fee_limit_sat BIGINT NOT NULL,
  routing_budget_ppm BIGINT,
  receiver_user_id BIGINT,
  decision TEXT NOT NULL,
  normalized_status TEXT NOT NULL,
  rejection_reason TEXT,
  lnd_failure_reason_code INTEGER,
  lnd_failure_reason TEXT,
  lnd_fee_msat BIGINT,
  policy_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
  request_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS payout_policy_attempts_order_idx
ON payout_policy_attempts(order_id);

CREATE INDEX IF NOT EXISTS payout_policy_attempts_payment_hash_idx
ON payout_policy_attempts(payment_hash);

CREATE TABLE IF NOT EXISTS payout_policy_onchain_attempts (
  id BIGSERIAL PRIMARY KEY,
  request_id TEXT UNIQUE,
  order_id BIGINT NOT NULL,
  onchain_payment_id BIGINT NOT NULL UNIQUE,
  address TEXT NOT NULL,
  amount_sats BIGINT NOT NULL,
  fee_rate_sat_vbyte BIGINT NOT NULL,
  mining_fee_sats BIGINT,
  gross_payout_sats BIGINT,
  decision TEXT NOT NULL,
  normalized_status TEXT NOT NULL,
  txid TEXT,
  rejection_reason TEXT,
  policy_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
  request_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS payout_policy_onchain_attempts_order_idx
ON payout_policy_onchain_attempts(order_id);

CREATE INDEX IF NOT EXISTS payout_policy_onchain_attempts_txid_idx
ON payout_policy_onchain_attempts(txid);

CREATE TABLE IF NOT EXISTS payout_policy_keysend_attempts (
  id BIGSERIAL PRIMARY KEY,
  request_id TEXT NOT NULL UNIQUE,
  order_id BIGINT NOT NULL,
  target_pubkey TEXT NOT NULL,
  message TEXT NOT NULL,
  amount_sats BIGINT NOT NULL,
  fee_limit_sat BIGINT NOT NULL,
  timeout_seconds BIGINT NOT NULL,
  payment_hash TEXT NOT NULL UNIQUE,
  preimage TEXT NOT NULL UNIQUE,
  decision TEXT NOT NULL,
  normalized_status TEXT NOT NULL,
  rejection_reason TEXT,
  lnd_failure_reason_code INTEGER,
  lnd_failure_reason TEXT,
  lnd_fee_msat BIGINT,
  policy_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
  request_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS payout_policy_keysend_attempts_order_idx
ON payout_policy_keysend_attempts(order_id);

CREATE INDEX IF NOT EXISTS payout_policy_keysend_attempts_payment_hash_idx
ON payout_policy_keysend_attempts(payment_hash);

CREATE TABLE IF NOT EXISTS payout_policy_reward_sources (
  id BIGSERIAL PRIMARY KEY,
  request_id TEXT NOT NULL UNIQUE,
  source_order_id BIGINT NOT NULL,
  receiver_robot_id BIGINT NOT NULL,
  receiver_user_id BIGINT NOT NULL,
  receiver_robot_name TEXT NOT NULL,
  reward_sats BIGINT NOT NULL,
  slashed_bond_payment_hash TEXT NOT NULL,
  slashed_bond_sats BIGINT NOT NULL,
  slashed_bond_status BIGINT NOT NULL,
  staked_bond_payment_hash TEXT NOT NULL,
  staked_bond_sats BIGINT NOT NULL,
  staked_bond_status BIGINT NOT NULL,
  reason TEXT NOT NULL,
  source_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (
    source_order_id,
    receiver_robot_id,
    slashed_bond_payment_hash,
    staked_bond_payment_hash,
    reason
  )
);

CREATE INDEX IF NOT EXISTS payout_policy_reward_sources_receiver_idx
ON payout_policy_reward_sources(receiver_user_id);

CREATE INDEX IF NOT EXISTS payout_policy_reward_sources_source_order_idx
ON payout_policy_reward_sources(source_order_id);

CREATE INDEX IF NOT EXISTS payout_policy_reward_sources_slashed_bond_idx
ON payout_policy_reward_sources(slashed_bond_payment_hash);

CREATE INDEX IF NOT EXISTS payout_policy_reward_sources_staked_bond_idx
ON payout_policy_reward_sources(staked_bond_payment_hash);

CREATE TABLE IF NOT EXISTS payout_policy_reward_attempts (
  id BIGSERIAL PRIMARY KEY,
  request_id TEXT NOT NULL UNIQUE,
  reward_receiver_user_id BIGINT NOT NULL,
  source_order_id BIGINT NOT NULL,
  payment_hash TEXT NOT NULL UNIQUE,
  amount_sats BIGINT NOT NULL,
  earned_rewards BIGINT NOT NULL,
  fee_limit_sat BIGINT NOT NULL,
  timeout_seconds BIGINT NOT NULL,
  decision TEXT NOT NULL,
  normalized_status TEXT NOT NULL,
  payment_preimage TEXT,
  rejection_reason TEXT,
  lnd_failure_reason_code INTEGER,
  lnd_failure_reason TEXT,
  lnd_fee_msat BIGINT,
  policy_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
  request_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE payout_policy_reward_attempts
DROP CONSTRAINT IF EXISTS payout_policy_reward_attempts_reward_receiver_user_id_source_order_id_key;

CREATE INDEX IF NOT EXISTS payout_policy_reward_attempts_source_order_idx
ON payout_policy_reward_attempts(source_order_id);

CREATE INDEX IF NOT EXISTS payout_policy_reward_attempts_payment_hash_idx
ON payout_policy_reward_attempts(payment_hash);
