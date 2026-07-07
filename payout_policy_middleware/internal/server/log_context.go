package server

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"

	"payout_policy_middleware/internal/httpapi"
	"payout_policy_middleware/internal/robosats"
)

func annotateLNPayoutRequest(r *http.Request, req httpapi.LNPayoutRequest) {
	setLogField(r, "kind", req.Kind)
	setLogFieldInt64(r, "order_id", req.OrderID)
	setLogField(r, "payment_hash", req.PaymentHash)
	setLogFieldInt64(r, "amount_sats", req.AmountSats)
	setLogFieldInt64Ptr(r, "receiver_user_id", req.ReceiverUserID)
	setLogField(r, "order_type", orderTypeLabel(req.OrderType))
	setLogFieldInt64(r, "order_status", req.OrderStatus)
}

func annotateOnchainPayoutRequest(r *http.Request, req httpapi.OnchainPayoutRequest) {
	setLogField(r, "kind", req.Kind)
	setLogFieldInt64(r, "order_id", req.OrderID)
	setLogFieldInt64(r, "onchain_payment_id", req.OnchainPaymentID)
	setLogFieldInt64(r, "amount_sats", req.AmountSats)
	setLogFieldInt64Ptr(r, "receiver_user_id", req.ReceiverUserID)
	setLogFieldInt64Ptr(r, "receiver_robot_id", req.ReceiverRobotID)
	setLogField(r, "onchain_concept", req.Concept)
	setLogField(r, "is_swap", req.IsSwap)
}

func annotateKeysendRequest(r *http.Request, req httpapi.KeysendRequest) {
	setLogField(r, "kind", req.Kind)
	setLogFieldInt64(r, "order_id", req.OrderID)
	setLogFieldInt64(r, "amount_sats", req.AmountSats)
}

func annotateRewardSourceRequest(r *http.Request, req httpapi.RewardSourceRequest) {
	setLogField(r, "kind", req.Kind)
	setLogFieldInt64(r, "order_id", req.SourceOrderID)
	setLogFieldInt64(r, "robot_id", req.ReceiverRobotID)
	setLogFieldInt64(r, "receiver_user_id", req.ReceiverUserID)
	setLogField(r, "robot", req.ReceiverRobotName)
	setLogFieldInt64(r, "reward_sats", req.RewardSats)
	setLogField(r, "reward_reason", req.Reason)
}

func annotateRewardWithdrawalRequest(r *http.Request, req httpapi.RewardWithdrawalRequest) {
	setLogField(r, "kind", req.Kind)
	setLogField(r, "payment_hash", req.PaymentHash)
	setLogFieldInt64(r, "amount_sats", req.AmountSats)
	setLogFieldInt64Ptr(r, "receiver_user_id", req.ReceiverUserID)
	setLogField(r, "robot", req.ReceiverRobotName)
}

func (s *Server) annotateLNPaymentByHash(r *http.Request, paymentHash string) {
	setLogField(r, "payment_hash", paymentHash)
	if s == nil || s.robosats == nil || paymentHash == "" {
		return
	}
	cacheKey := "hash:" + paymentHash
	if row, ok := s.logCtx.get(cacheKey); ok {
		annotateLNPaymentLogContext(r, row)
		return
	}
	row, err := s.robosats.FetchLNPaymentLogContextByPaymentHash(r.Context(), paymentHash)
	if err != nil {
		log.Printf("LN payment log context lookup failed: %v", err)
		return
	}
	s.logCtx.set(cacheKey, row)
	annotateLNPaymentLogContext(r, row)
}

func (s *Server) annotateLNPaymentByPreimage(r *http.Request, preimage string) {
	if s == nil || s.robosats == nil || preimage == "" {
		return
	}
	cacheKey := "preimage:" + preimage
	if row, ok := s.logCtx.get(cacheKey); ok {
		annotateLNPaymentLogContext(r, row)
		return
	}
	row, err := s.robosats.FetchLNPaymentLogContextByPreimage(r.Context(), preimage)
	if err != nil {
		log.Printf("LN payment log context lookup failed: %v", err)
		return
	}
	s.logCtx.set(cacheKey, row)
	annotateLNPaymentLogContext(r, row)
}

func annotateLNPaymentLogContext(r *http.Request, row *robosats.LNPaymentLogContext) {
	if row == nil {
		return
	}
	setLogField(r, "payment_hash", row.PaymentHash)
	setLogField(r, "invoice_type", lnPaymentTypeLabel(row.Type))
	setLogField(r, "invoice_concept", lnPaymentConceptLabel(row.Concept))
	setLogField(r, "invoice_status", lnPaymentStatusLabel(row.Status))
	setLogFieldInt64(r, "amount_sats", row.NumSatoshis)
	setLogFieldNullInt64(r, "order_id", row.OrderID)
	setLogFieldNullInt64(r, "order_status", row.OrderStatus)
	if row.OrderType.Valid {
		setLogField(r, "order_type", orderTypeLabel(row.OrderType.Int64))
	}

	userID, robotID, username := activeLNPaymentRobot(row)
	setLogFieldNullInt64(r, "user_id", userID)
	setLogFieldNullInt64(r, "robot_id", robotID)
	setLogFieldNullString(r, "robot", username)
}

func activeLNPaymentRobot(row *robosats.LNPaymentLogContext) (sql.NullInt64, sql.NullInt64, sql.NullString) {
	switch row.Concept {
	case 3, 4:
		return row.ReceiverUserID, row.ReceiverRobotID, row.ReceiverUsername
	default:
		return row.SenderUserID, row.SenderRobotID, row.SenderUsername
	}
}

func setLogFieldNullInt64(r *http.Request, key string, value sql.NullInt64) {
	if !value.Valid {
		return
	}
	setLogField(r, key, value.Int64)
}

func setLogFieldNullString(r *http.Request, key string, value sql.NullString) {
	if !value.Valid {
		return
	}
	setLogField(r, key, value.String)
}

func lnPaymentTypeLabel(value int64) string {
	switch value {
	case 0:
		return "NORM"
	case 1:
		return "HOLD"
	case 2:
		return "KEYS"
	default:
		return fmt.Sprintf("UNKNOWN_%d", value)
	}
}

func lnPaymentConceptLabel(value int64) string {
	switch value {
	case 0:
		return "MAKEBOND"
	case 1:
		return "TAKEBOND"
	case 2:
		return "TRESCROW"
	case 3:
		return "PAYBUYER"
	case 4:
		return "WITHREWA"
	case 5:
		return "DEVDONAT"
	default:
		return fmt.Sprintf("UNKNOWN_%d", value)
	}
}

func lnPaymentStatusLabel(value int64) string {
	switch value {
	case 0:
		return "INVGEN"
	case 1:
		return "LOCKED"
	case 2:
		return "SETLED"
	case 3:
		return "RETNED"
	case 4:
		return "CANCEL"
	case 5:
		return "EXPIRE"
	case 6:
		return "VALIDI"
	case 7:
		return "FLIGHT"
	case 8:
		return "SUCCED"
	case 9:
		return "FAILRO"
	default:
		return fmt.Sprintf("UNKNOWN_%d", value)
	}
}

func orderTypeLabel(value int64) string {
	switch value {
	case 0:
		return "BUY"
	case 1:
		return "SELL"
	default:
		return fmt.Sprintf("UNKNOWN_%d", value)
	}
}
