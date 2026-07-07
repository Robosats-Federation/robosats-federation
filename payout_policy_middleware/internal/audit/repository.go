package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"payout_policy_middleware/internal/config"
	"payout_policy_middleware/internal/httpapi"
	"payout_policy_middleware/internal/robosats"
)

type Repository struct {
	pool *pgxpool.Pool
	cfg  config.Config
}

type OnchainAttempt struct {
	RequestID        sql.NullString
	OrderID          int64
	OnchainPaymentID int64
	Address          string
	AmountSats       int64
	FeeRateSatVByte  int64
	TxID             sql.NullString
}

type KeysendAttempt struct {
	RequestID            string
	OrderID              int64
	TargetPubkey         string
	Message              string
	AmountSats           int64
	FeeLimitSat          int64
	TimeoutSeconds       int64
	PaymentHash          string
	Preimage             string
	Decision             string
	NormalizedStatus     string
	LNDFeeMsat           sql.NullInt64
	LNDFailureReasonCode sql.NullInt64
	LNDFailureReason     sql.NullString
}

type RewardAttempt struct {
	RequestID            string
	RewardReceiverUserID int64
	SourceOrderID        int64
	PaymentHash          string
	AmountSats           int64
	EarnedRewards        int64
	FeeLimitSat          int64
	TimeoutSeconds       int64
	Decision             string
	NormalizedStatus     string
	PaymentPreimage      sql.NullString
	LNDFeeMsat           sql.NullInt64
	LNDFailureReasonCode sql.NullInt64
	LNDFailureReason     sql.NullString
}

func NewRepository(pool *pgxpool.Pool, cfg config.Config) *Repository {
	return &Repository{pool: pool, cfg: cfg}
}

func (r *Repository) AuditLN(ctx context.Context, req httpapi.LNPayoutRequest, result httpapi.PaymentResponse, rejectionReason *string) error {
	policySnapshot := mustJSON(map[string]any{"max_payout_sats": r.cfg.MaxPayoutSats})
	requestSnapshot := mustJSON(req)
	_, err := r.pool.Exec(ctx, `
            INSERT INTO payout_policy_attempts (
              request_id, order_id, payment_hash, amount_sats, fee_limit_sat,
              routing_budget_ppm, receiver_user_id, decision, normalized_status,
              rejection_reason, lnd_failure_reason_code, lnd_failure_reason,
              lnd_fee_msat, policy_snapshot, request_snapshot
            ) VALUES (
              $1, $2, $3, $4, $5,
              $6, $7, $8, $9, $10,
              $11, $12, $13, $14::jsonb, $15::jsonb
            )
            ON CONFLICT (request_id) DO UPDATE SET
              decision = EXCLUDED.decision,
              normalized_status = EXCLUDED.normalized_status,
              rejection_reason = EXCLUDED.rejection_reason,
              lnd_failure_reason_code = EXCLUDED.lnd_failure_reason_code,
              lnd_failure_reason = EXCLUDED.lnd_failure_reason,
              lnd_fee_msat = EXCLUDED.lnd_fee_msat,
              policy_snapshot = EXCLUDED.policy_snapshot,
              request_snapshot = EXCLUDED.request_snapshot,
              updated_at = now();`,
		req.RequestID,
		req.OrderID,
		req.PaymentHash,
		req.AmountSats,
		req.FeeLimitSat,
		req.RoutingBudgetPPM,
		req.ReceiverUserID,
		result.Decision,
		result.Status,
		rejectionReason,
		result.FailureReasonCode,
		result.FailureReason,
		result.FeeMsat,
		policySnapshot,
		requestSnapshot,
	)
	return err
}

func (r *Repository) ReserveOnchainAttempt(ctx context.Context, req httpapi.OnchainPayoutRequest) (bool, *OnchainAttempt, error) {
	policySnapshot := mustJSON(map[string]any{
		"max_onchain_payout_sats": r.cfg.MaxOnchainPayoutSats,
		"allow_spend_unconfirmed": r.cfg.AllowSpendUnconfirmed,
	})
	requestSnapshot := mustJSON(req)

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, nil, err
	}
	defer tx.Rollback(ctx)

	var inserted OnchainAttempt
	err = tx.QueryRow(ctx, `
                INSERT INTO payout_policy_onchain_attempts (
                  request_id, order_id, onchain_payment_id, address, amount_sats,
                  fee_rate_sat_vbyte, mining_fee_sats, gross_payout_sats,
                  decision, normalized_status, policy_snapshot, request_snapshot
                ) VALUES (
                  $1, $2, $3, $4, $5, $6, $7, $8,
                  'APPROVED', 'INITIATED', $9::jsonb, $10::jsonb
                )
                ON CONFLICT (onchain_payment_id) DO NOTHING
                RETURNING request_id, order_id, onchain_payment_id, address, amount_sats, fee_rate_sat_vbyte, txid;`,
		req.RequestID,
		req.OrderID,
		req.OnchainPaymentID,
		req.Address,
		req.AmountSats,
		req.FeeRateSatVByte,
		req.MiningFeeSats,
		req.GrossPayoutSats,
		policySnapshot,
		requestSnapshot,
	).Scan(
		&inserted.RequestID,
		&inserted.OrderID,
		&inserted.OnchainPaymentID,
		&inserted.Address,
		&inserted.AmountSats,
		&inserted.FeeRateSatVByte,
		&inserted.TxID,
	)
	if err == nil {
		return true, &inserted, tx.Commit(ctx)
	}
	if err != pgx.ErrNoRows {
		return false, nil, err
	}

	existing, err := scanOnchainAttempt(ctx, tx, req.OnchainPaymentID)
	if err != nil {
		return false, nil, err
	}
	return false, existing, tx.Commit(ctx)
}

func (r *Repository) UpdateOnchainAttempt(ctx context.Context, onchainPaymentID int64, result any, rejectionReason *string) error {
	decision, status, txid := resultFields(result)
	_, err := r.pool.Exec(ctx, `
            UPDATE payout_policy_onchain_attempts
            SET decision = $1,
                normalized_status = $2,
                txid = COALESCE($3, txid),
                rejection_reason = $4,
                updated_at = now()
            WHERE onchain_payment_id = $5;`,
		decision, status, nullableString(txid), rejectionReason, onchainPaymentID,
	)
	return err
}

func (r *Repository) ReserveKeysendAttempt(ctx context.Context, req httpapi.KeysendRequest, preimage, paymentHash string) (bool, *KeysendAttempt, error) {
	policySnapshot := mustJSON(map[string]any{
		"max_devfund_keysend_sats": r.cfg.MaxDevfundKeysendSats,
		"devfund_pubkey":           r.cfg.DevfundPubkey,
	})
	requestSnapshot := mustJSON(req)

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, nil, err
	}
	defer tx.Rollback(ctx)

	var inserted KeysendAttempt
	err = tx.QueryRow(ctx, `
                INSERT INTO payout_policy_keysend_attempts (
                  request_id, order_id, target_pubkey, message, amount_sats,
                  fee_limit_sat, timeout_seconds, payment_hash, preimage,
                  decision, normalized_status, policy_snapshot, request_snapshot
                ) VALUES (
                  $1, $2, $3, $4, $5, $6, $7, $8, $9,
                  'APPROVED', 'INITIATED', $10::jsonb, $11::jsonb
                )
                ON CONFLICT (request_id) DO NOTHING
                RETURNING request_id, order_id, target_pubkey, message, amount_sats,
                          fee_limit_sat, timeout_seconds, payment_hash, preimage,
                          decision, normalized_status, lnd_fee_msat,
                          lnd_failure_reason_code, lnd_failure_reason;`,
		req.RequestID,
		req.OrderID,
		req.TargetPubkey,
		req.Message,
		req.AmountSats,
		req.FeeLimitSat,
		req.TimeoutSeconds,
		paymentHash,
		preimage,
		policySnapshot,
		requestSnapshot,
	).Scan(
		&inserted.RequestID,
		&inserted.OrderID,
		&inserted.TargetPubkey,
		&inserted.Message,
		&inserted.AmountSats,
		&inserted.FeeLimitSat,
		&inserted.TimeoutSeconds,
		&inserted.PaymentHash,
		&inserted.Preimage,
		&inserted.Decision,
		&inserted.NormalizedStatus,
		&inserted.LNDFeeMsat,
		&inserted.LNDFailureReasonCode,
		&inserted.LNDFailureReason,
	)
	if err == nil {
		return true, &inserted, tx.Commit(ctx)
	}
	if err != pgx.ErrNoRows {
		return false, nil, err
	}

	existing, err := scanKeysendAttempt(ctx, tx, req.RequestID)
	if err != nil {
		return false, nil, err
	}
	return false, existing, tx.Commit(ctx)
}

func (r *Repository) UpdateKeysendAttempt(ctx context.Context, requestID string, result httpapi.PaymentResponse, rejectionReason *string) error {
	_, err := r.pool.Exec(ctx, `
            UPDATE payout_policy_keysend_attempts
            SET decision = $1,
                normalized_status = $2,
                lnd_fee_msat = $3,
                lnd_failure_reason_code = $4,
                lnd_failure_reason = $5,
                rejection_reason = $6,
                updated_at = now()
            WHERE request_id = $7;`,
		result.Decision,
		result.Status,
		result.FeeMsat,
		result.FailureReasonCode,
		result.FailureReason,
		rejectionReason,
		requestID,
	)
	return err
}

func (r *Repository) ReserveRewardAttempt(ctx context.Context, req httpapi.LNPayoutRequest, row *robosats.RewardRow) (bool, *RewardAttempt, error) {
	policySnapshot := mustJSON(map[string]any{
		"max_reward_withdrawal_sats":     r.cfg.MaxRewardWithdrawalSats,
		"reward_routing_fee_limit_ppm":   r.cfg.RewardRoutingFeeLimitPPM,
		"reward_min_flat_fee_limit_sats": r.cfg.RewardMinFlatFeeLimitSats,
	})
	requestSnapshot := mustJSON(req)

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, nil, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1);`, row.ReceiverUserID.Int64); err != nil {
		return false, nil, err
	}

	existing, err := scanRewardAttempt(ctx, tx, req.RequestID, req.PaymentHash)
	if err == nil {
		return false, existing, tx.Commit(ctx)
	}
	if err != pgx.ErrNoRows {
		return false, nil, err
	}

	recent, err := scanRecentRewardAttempt(ctx, tx, row.ReceiverUserID.Int64, time.Now().UTC().Add(-rewardCooldown(r.cfg)))
	if err == nil {
		return false, recent, tx.Commit(ctx)
	}
	if err != pgx.ErrNoRows {
		return false, nil, err
	}

	var inserted RewardAttempt
	err = tx.QueryRow(ctx, `
                INSERT INTO payout_policy_reward_attempts (
                  request_id, reward_receiver_user_id, source_order_id, payment_hash,
                  amount_sats, earned_rewards, fee_limit_sat, timeout_seconds,
                  decision, normalized_status, policy_snapshot, request_snapshot
                ) VALUES (
                  $1, $2, $3, $4, $5, $6, $7, $8,
                  'APPROVED', 'INITIATED', $9::jsonb, $10::jsonb
                )
                ON CONFLICT DO NOTHING
                RETURNING request_id, reward_receiver_user_id, source_order_id,
                          payment_hash, amount_sats, earned_rewards, fee_limit_sat,
                          timeout_seconds, decision, normalized_status, payment_preimage,
                          lnd_fee_msat, lnd_failure_reason_code, lnd_failure_reason;`,
		req.RequestID,
		row.ReceiverUserID.Int64,
		row.SourceOrderID,
		req.PaymentHash,
		req.AmountSats,
		row.EarnedRewards,
		req.FeeLimitSat,
		req.TimeoutSeconds,
		policySnapshot,
		requestSnapshot,
	).Scan(
		&inserted.RequestID,
		&inserted.RewardReceiverUserID,
		&inserted.SourceOrderID,
		&inserted.PaymentHash,
		&inserted.AmountSats,
		&inserted.EarnedRewards,
		&inserted.FeeLimitSat,
		&inserted.TimeoutSeconds,
		&inserted.Decision,
		&inserted.NormalizedStatus,
		&inserted.PaymentPreimage,
		&inserted.LNDFeeMsat,
		&inserted.LNDFailureReasonCode,
		&inserted.LNDFailureReason,
	)
	if err == nil {
		return true, &inserted, tx.Commit(ctx)
	}
	if err != pgx.ErrNoRows {
		return false, nil, err
	}

	existing, err = scanRewardAttempt(ctx, tx, req.RequestID, req.PaymentHash)
	if err != nil {
		return false, nil, err
	}
	return false, existing, tx.Commit(ctx)
}

func (r *Repository) UpdateRewardAttempt(ctx context.Context, requestID string, result httpapi.PaymentResponse, rejectionReason *string) error {
	_, err := r.pool.Exec(ctx, `
            UPDATE payout_policy_reward_attempts
            SET decision = $1,
                normalized_status = $2,
                payment_preimage = COALESCE(NULLIF($3, ''), payment_preimage),
                lnd_fee_msat = $4,
                lnd_failure_reason_code = $5,
                lnd_failure_reason = $6,
                rejection_reason = $7,
                updated_at = now()
            WHERE request_id = $8;`,
		result.Decision,
		result.Status,
		result.PaymentPreimage,
		result.FeeMsat,
		result.FailureReasonCode,
		result.FailureReason,
		rejectionReason,
		requestID,
	)
	return err
}

func (r *Repository) RecordRewardSource(ctx context.Context, req httpapi.RewardSourceRequest) error {
	sourceSnapshot := mustJSON(req)
	_, err := r.pool.Exec(ctx, `
            INSERT INTO payout_policy_reward_sources (
              request_id, source_order_id, receiver_robot_id, receiver_user_id,
              receiver_robot_name, reward_sats, slashed_bond_payment_hash,
              slashed_bond_sats, slashed_bond_status, staked_bond_payment_hash,
              staked_bond_sats, staked_bond_status, reason, source_snapshot
            ) VALUES (
              $1, $2, $3, $4,
              $5, $6, $7,
              $8, $9, $10,
              $11, $12, $13, $14::jsonb
            )
            ON CONFLICT DO NOTHING;`,
		req.RequestID,
		req.SourceOrderID,
		req.ReceiverRobotID,
		req.ReceiverUserID,
		req.ReceiverRobotName,
		req.RewardSats,
		req.SlashedBondPaymentHash,
		req.SlashedBondSats,
		req.SlashedBondStatus,
		req.StakedBondPaymentHash,
		req.StakedBondSats,
		req.StakedBondStatus,
		req.Reason,
		sourceSnapshot,
	)
	return err
}

func (r *Repository) FetchRewardSources(ctx context.Context, receiverUserID int64) ([]robosats.RewardSource, error) {
	rows, err := r.pool.Query(ctx, `
            SELECT source_order_id, receiver_robot_id, receiver_user_id,
                   receiver_robot_name, reward_sats, slashed_bond_payment_hash,
                   slashed_bond_sats, slashed_bond_status, staked_bond_payment_hash,
                   staked_bond_sats, staked_bond_status, reason
            FROM payout_policy_reward_sources
            WHERE receiver_user_id = $1
            ORDER BY created_at ASC, id ASC;`, receiverUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []robosats.RewardSource
	for rows.Next() {
		var row robosats.RewardSource
		if err := rows.Scan(
			&row.SourceOrderID,
			&row.ReceiverRobotID,
			&row.ReceiverUserID,
			&row.ReceiverRobotName,
			&row.RewardSats,
			&row.SlashedBondPaymentHash,
			&row.SlashedBondSats,
			&row.SlashedBondStatus,
			&row.StakedBondPaymentHash,
			&row.StakedBondSats,
			&row.StakedBondStatus,
			&row.Reason,
		); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repository) FetchSuccessfulRewardAttemptEarnedTotal(ctx context.Context, receiverUserID int64) (int64, error) {
	var total int64
	err := r.pool.QueryRow(ctx, `
            SELECT COALESCE(SUM(earned_rewards), 0)
            FROM payout_policy_reward_attempts
            WHERE reward_receiver_user_id = $1
              AND normalized_status IN ('SUCCEEDED', 'ALREADY_PAID');`, receiverUserID).Scan(&total)
	if err != nil {
		return 0, err
	}
	return total, nil
}

func KeysendAttemptResult(row *KeysendAttempt) httpapi.PaymentResponse {
	feeMsat := int64(0)
	if row.LNDFeeMsat.Valid {
		feeMsat = row.LNDFeeMsat.Int64
	}
	var failureCode *int64
	if row.LNDFailureReasonCode.Valid {
		value := row.LNDFailureReasonCode.Int64
		failureCode = &value
	}
	var failureReason *string
	if row.LNDFailureReason.Valid {
		value := row.LNDFailureReason.String
		failureReason = &value
	}
	return httpapi.PaymentResponse{
		Decision:          row.Decision,
		Status:            row.NormalizedStatus,
		PaymentHash:       row.PaymentHash,
		PaymentPreimage:   row.Preimage,
		FeeMsat:           feeMsat,
		FeeSat:            float64(feeMsat) / 1000,
		FailureReasonCode: failureCode,
		FailureReason:     failureReason,
		Message:           "already handled",
	}
}

func RewardAttemptResult(row *RewardAttempt) httpapi.PaymentResponse {
	feeMsat := int64(0)
	if row.LNDFeeMsat.Valid {
		feeMsat = row.LNDFeeMsat.Int64
	}
	var failureCode *int64
	if row.LNDFailureReasonCode.Valid {
		value := row.LNDFailureReasonCode.Int64
		failureCode = &value
	}
	var failureReason *string
	if row.LNDFailureReason.Valid {
		value := row.LNDFailureReason.String
		failureReason = &value
	}
	preimage := ""
	if row.PaymentPreimage.Valid {
		preimage = row.PaymentPreimage.String
	}
	return httpapi.PaymentResponse{
		Decision:          row.Decision,
		Status:            row.NormalizedStatus,
		PaymentHash:       row.PaymentHash,
		PaymentPreimage:   preimage,
		FeeMsat:           feeMsat,
		FeeSat:            float64(feeMsat) / 1000,
		FailureReasonCode: failureCode,
		FailureReason:     failureReason,
		Message:           "already handled",
	}
}

func scanRewardAttempt(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, requestID string, paymentHash string) (*RewardAttempt, error) {
	var row RewardAttempt
	err := q.QueryRow(ctx, `
                SELECT request_id, reward_receiver_user_id, source_order_id,
                       payment_hash, amount_sats, earned_rewards, fee_limit_sat,
                       timeout_seconds, decision, normalized_status, payment_preimage,
                       lnd_fee_msat, lnd_failure_reason_code, lnd_failure_reason
                FROM payout_policy_reward_attempts
                WHERE request_id = $1
                   OR payment_hash = $2
                ORDER BY created_at ASC
                LIMIT 1;`, requestID, paymentHash).Scan(
		&row.RequestID,
		&row.RewardReceiverUserID,
		&row.SourceOrderID,
		&row.PaymentHash,
		&row.AmountSats,
		&row.EarnedRewards,
		&row.FeeLimitSat,
		&row.TimeoutSeconds,
		&row.Decision,
		&row.NormalizedStatus,
		&row.PaymentPreimage,
		&row.LNDFeeMsat,
		&row.LNDFailureReasonCode,
		&row.LNDFailureReason,
	)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func scanRecentRewardAttempt(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, receiverUserID int64, since time.Time) (*RewardAttempt, error) {
	var row RewardAttempt
	err := q.QueryRow(ctx, `
                SELECT request_id, reward_receiver_user_id, source_order_id,
                       payment_hash, amount_sats, earned_rewards, fee_limit_sat,
                       timeout_seconds, decision, normalized_status, payment_preimage,
                       lnd_fee_msat, lnd_failure_reason_code, lnd_failure_reason
                FROM payout_policy_reward_attempts
                WHERE reward_receiver_user_id = $1
                  AND updated_at >= $2
                  AND normalized_status IN ('SUCCEEDED', 'ALREADY_PAID', 'INITIATED', 'IN_FLIGHT')
                ORDER BY updated_at DESC
                LIMIT 1;`, receiverUserID, since).Scan(
		&row.RequestID,
		&row.RewardReceiverUserID,
		&row.SourceOrderID,
		&row.PaymentHash,
		&row.AmountSats,
		&row.EarnedRewards,
		&row.FeeLimitSat,
		&row.TimeoutSeconds,
		&row.Decision,
		&row.NormalizedStatus,
		&row.PaymentPreimage,
		&row.LNDFeeMsat,
		&row.LNDFailureReasonCode,
		&row.LNDFailureReason,
	)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func rewardCooldown(cfg config.Config) time.Duration {
	hours := cfg.RewardWithdrawalCooldownHours
	if hours <= 0 {
		hours = 48
	}
	return time.Duration(hours) * time.Hour
}

func scanOnchainAttempt(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, onchainPaymentID int64) (*OnchainAttempt, error) {
	var row OnchainAttempt
	err := q.QueryRow(ctx, `
                SELECT request_id, order_id, onchain_payment_id, address, amount_sats,
                       fee_rate_sat_vbyte, txid
                FROM payout_policy_onchain_attempts
                WHERE onchain_payment_id = $1;`, onchainPaymentID).Scan(
		&row.RequestID,
		&row.OrderID,
		&row.OnchainPaymentID,
		&row.Address,
		&row.AmountSats,
		&row.FeeRateSatVByte,
		&row.TxID,
	)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func scanKeysendAttempt(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, requestID string) (*KeysendAttempt, error) {
	var row KeysendAttempt
	err := q.QueryRow(ctx, `
                SELECT request_id, order_id, target_pubkey, message, amount_sats,
                       fee_limit_sat, timeout_seconds, payment_hash, preimage,
                       decision, normalized_status, lnd_fee_msat,
                       lnd_failure_reason_code, lnd_failure_reason
                FROM payout_policy_keysend_attempts
                WHERE request_id = $1;`, requestID).Scan(
		&row.RequestID,
		&row.OrderID,
		&row.TargetPubkey,
		&row.Message,
		&row.AmountSats,
		&row.FeeLimitSat,
		&row.TimeoutSeconds,
		&row.PaymentHash,
		&row.Preimage,
		&row.Decision,
		&row.NormalizedStatus,
		&row.LNDFeeMsat,
		&row.LNDFailureReasonCode,
		&row.LNDFailureReason,
	)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func mustJSON(value any) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}

func nullableString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func resultFields(result any) (decision string, status string, txid string) {
	switch value := result.(type) {
	case httpapi.OnchainResponse:
		return value.Decision, value.Status, value.TxID
	case *httpapi.OnchainResponse:
		return value.Decision, value.Status, value.TxID
	case map[string]any:
		decision, _ = value["decision"].(string)
		status, _ = value["status"].(string)
		txid, _ = value["txid"].(string)
		return decision, status, txid
	default:
		return "ERROR", "ERROR", ""
	}
}
