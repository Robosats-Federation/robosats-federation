package robosats

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) FetchLNRow(ctx context.Context, orderID int64, paymentHash string) (*LNRow, error) {
	const query = `
    SELECT
      o.id AS order_id,
      o.status AS order_status,
      o.type AS order_type,
      o.is_swap,
      o.payout_id,
      o.trade_escrow_id,
      o.maker_bond_id,
      o.taker_bond_id,
      o.maker_id,
      o.taker_id,
      o.last_satoshis,
      o.proceeds,
      p.payment_hash AS payout_hash,
      p.status AS payout_status,
      p.type AS payout_type,
      p.concept AS payout_concept,
      p.num_satoshis AS payout_sats,
      p.routing_budget_ppm,
      p.routing_budget_sats::text,
      p.receiver_id AS payout_receiver_id,
      p.sender_id AS payout_sender_id,
      p.invoice AS payout_invoice,
      p.expires_at AS payout_expires_at,
      e.status AS escrow_status,
      e.num_satoshis AS escrow_sats,
      mb.status AS maker_bond_status,
      tb.status AS taker_bond_status
    FROM api_order o
    JOIN api_lnpayment p ON p.payment_hash = o.payout_id
    JOIN api_lnpayment e ON e.payment_hash = o.trade_escrow_id
    LEFT JOIN api_lnpayment mb ON mb.payment_hash = o.maker_bond_id
    LEFT JOIN api_lnpayment tb ON tb.payment_hash = o.taker_bond_id
    WHERE o.id = $1
      AND p.payment_hash = $2;`

	var row LNRow
	err := r.pool.QueryRow(ctx, query, orderID, paymentHash).Scan(
		&row.OrderID,
		&row.OrderStatus,
		&row.OrderType,
		&row.IsSwap,
		&row.PayoutID,
		&row.TradeEscrowID,
		&row.MakerBondID,
		&row.TakerBondID,
		&row.MakerID,
		&row.TakerID,
		&row.LastSatoshis,
		&row.Proceeds,
		&row.PayoutHash,
		&row.PayoutStatus,
		&row.PayoutType,
		&row.PayoutConcept,
		&row.PayoutSats,
		&row.RoutingBudgetPPM,
		&row.RoutingBudgetSats,
		&row.PayoutReceiverID,
		&row.PayoutSenderID,
		&row.PayoutInvoice,
		&row.PayoutExpiresAt,
		&row.EscrowStatus,
		&row.EscrowSats,
		&row.MakerBondStatus,
		&row.TakerBondStatus,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *Repository) FetchOnchainRow(ctx context.Context, orderID int64, onchainPaymentID int64) (*OnchainRow, error) {
	const query = `
    SELECT
      o.id AS order_id,
      o.status AS order_status,
      o.type AS order_type,
      o.is_swap,
      o.payout_tx_id,
      o.trade_escrow_id,
      o.maker_id,
      o.taker_id,
      tx.id AS onchain_payment_id,
      tx.concept AS onchain_concept,
      tx.status AS onchain_status,
      tx.broadcasted,
      tx.address,
      tx.txid,
      tx.num_satoshis,
      tx.sent_satoshis,
      tx.mining_fee_rate::text,
      tx.mining_fee_sats,
      tx.swap_fee_rate::text,
      tx.receiver_id AS payout_receiver_id,
      e.status AS escrow_status,
      e.num_satoshis AS escrow_sats
    FROM api_order o
    JOIN api_onchainpayment tx ON tx.id = o.payout_tx_id
    JOIN api_lnpayment e ON e.payment_hash = o.trade_escrow_id
    WHERE o.id = $1
      AND tx.id = $2;`

	var row OnchainRow
	err := r.pool.QueryRow(ctx, query, orderID, onchainPaymentID).Scan(
		&row.OrderID,
		&row.OrderStatus,
		&row.OrderType,
		&row.IsSwap,
		&row.PayoutTxID,
		&row.TradeEscrowID,
		&row.MakerID,
		&row.TakerID,
		&row.OnchainPaymentID,
		&row.OnchainConcept,
		&row.OnchainStatus,
		&row.Broadcasted,
		&row.Address,
		&row.TxID,
		&row.NumSatoshis,
		&row.SentSatoshis,
		&row.MiningFeeRate,
		&row.MiningFeeSats,
		&row.SwapFeeRate,
		&row.PayoutReceiverID,
		&row.EscrowStatus,
		&row.EscrowSats,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *Repository) FetchDevfundOrder(ctx context.Context, orderID int64) (*DevfundOrder, error) {
	const query = `
    SELECT
      o.id AS order_id,
      o.status AS order_status,
      o.proceeds
    FROM api_order o
    WHERE o.id = $1;`

	var row DevfundOrder
	err := r.pool.QueryRow(ctx, query, orderID).Scan(&row.OrderID, &row.OrderStatus, &row.Proceeds)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func BuyerUserID(orderType int64, makerID, takerID sql.NullInt64) (sql.NullInt64, error) {
	switch orderType {
	case 0:
		return makerID, nil
	case 1:
		return takerID, nil
	default:
		return sql.NullInt64{}, fmt.Errorf("unknown order type")
	}
}

func (r *Repository) FetchRewardRow(ctx context.Context, paymentHash string) (*RewardRow, error) {
	const query = `
    SELECT
      r2.user_id AS receiver_user_id,
      r2.id AS receiver_robot_id,
      u.username AS receiver_username,
      r2.earned_rewards,
      p.payment_hash AS payout_hash,
      p.invoice AS payout_invoice,
      p.num_satoshis AS payout_sats,
      p.expires_at AS payout_expires_at,
      p.routing_budget_ppm,
      p.routing_budget_sats::text
    FROM api_lnpayment p
    JOIN api_robot r2 ON r2.user_id = p.receiver_id
    JOIN auth_user u ON u.id = p.receiver_id
    WHERE p.payment_hash = $1
      AND p.concept = 4;`

	var row RewardRow
	err := r.pool.QueryRow(ctx, query, paymentHash).Scan(
		&row.ReceiverUserID,
		&row.ReceiverRobotID,
		&row.ReceiverUsername,
		&row.EarnedRewards,
		&row.PayoutHash,
		&row.PayoutInvoice,
		&row.PayoutSats,
		&row.PayoutExpiresAt,
		&row.RoutingBudgetPPM,
		&row.RoutingBudgetSats,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *Repository) FetchRobotIdentity(ctx context.Context, userID int64) (*RobotIdentity, error) {
	const query = `
    SELECT r.user_id, r.id, u.username
    FROM api_robot r
    JOIN auth_user u ON u.id = r.user_id
    WHERE r.user_id = $1;`

	var row RobotIdentity
	err := r.pool.QueryRow(ctx, query, userID).Scan(&row.UserID, &row.RobotID, &row.Username)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *Repository) FetchOrderBonds(ctx context.Context, orderID int64) (*OrderBonds, error) {
	const query = `
    SELECT id, maker_id, taker_id, maker_bond_id, taker_bond_id
    FROM api_order
    WHERE id = $1;`

	var row OrderBonds
	err := r.pool.QueryRow(ctx, query, orderID).Scan(
		&row.OrderID,
		&row.MakerID,
		&row.TakerID,
		&row.MakerBondID,
		&row.TakerBondID,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *Repository) HasRecentSuccessfulRewardWithdrawal(ctx context.Context, receiverUserID int64, currentPaymentHash string, since time.Time) (bool, error) {
	const query = `
    SELECT EXISTS(
      SELECT 1
      FROM api_lnpayment
      WHERE receiver_id = $1
        AND concept = 4
        AND status = 8
        AND payment_hash <> $2
        AND created_at >= $3
    );`
	var exists bool
	err := r.pool.QueryRow(ctx, query, receiverUserID, currentPaymentHash, since).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (r *Repository) FetchBondByPaymentHash(ctx context.Context, paymentHash string) (*BondInfo, error) {
	const query = `
    SELECT payment_hash, num_satoshis, status, concept, sender_id
    FROM api_lnpayment
    WHERE payment_hash = $1;`

	var info BondInfo
	err := r.pool.QueryRow(ctx, query, paymentHash).Scan(
		&info.PaymentHash,
		&info.NumSatoshis,
		&info.Status,
		&info.Concept,
		&info.SenderID,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &info, nil
}

func (r *Repository) FetchLNPaymentLogContextByPaymentHash(ctx context.Context, paymentHash string) (*LNPaymentLogContext, error) {
	return r.fetchLNPaymentLogContext(ctx, "p.payment_hash = $1", paymentHash)
}

func (r *Repository) FetchLNPaymentLogContextByPreimage(ctx context.Context, preimage string) (*LNPaymentLogContext, error) {
	return r.fetchLNPaymentLogContext(ctx, "p.preimage = $1", preimage)
}

func (r *Repository) fetchLNPaymentLogContext(ctx context.Context, where string, value string) (*LNPaymentLogContext, error) {
	query := fmt.Sprintf(`
    SELECT
      p.payment_hash,
      p.type,
      p.concept,
      p.status,
      p.num_satoshis,
      p.sender_id,
      sr.id AS sender_robot_id,
      su.username AS sender_username,
      p.receiver_id,
      rr.id AS receiver_robot_id,
      ru.username AS receiver_username,
      o.id AS order_id,
      o.status AS order_status,
      o.type AS order_type
    FROM api_lnpayment p
    LEFT JOIN auth_user su ON su.id = p.sender_id
    LEFT JOIN api_robot sr ON sr.user_id = p.sender_id
    LEFT JOIN auth_user ru ON ru.id = p.receiver_id
    LEFT JOIN api_robot rr ON rr.user_id = p.receiver_id
    LEFT JOIN api_order o ON (
      o.maker_bond_id = p.payment_hash
      OR o.taker_bond_id = p.payment_hash
      OR o.trade_escrow_id = p.payment_hash
      OR o.payout_id = p.payment_hash
    )
    WHERE %s
    ORDER BY o.id DESC NULLS LAST
    LIMIT 1;`, where)

	var row LNPaymentLogContext
	err := r.pool.QueryRow(ctx, query, value).Scan(
		&row.PaymentHash,
		&row.Type,
		&row.Concept,
		&row.Status,
		&row.NumSatoshis,
		&row.SenderUserID,
		&row.SenderRobotID,
		&row.SenderUsername,
		&row.ReceiverUserID,
		&row.ReceiverRobotID,
		&row.ReceiverUsername,
		&row.OrderID,
		&row.OrderStatus,
		&row.OrderType,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}
