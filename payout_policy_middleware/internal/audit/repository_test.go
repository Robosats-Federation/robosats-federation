package audit

import (
	"database/sql"
	"testing"

	"payout_policy_middleware/internal/httpapi"
)

func TestKeysendAttemptResult(t *testing.T) {
	row := &KeysendAttempt{
		RequestID:            "keysend-devfund-test",
		OrderID:              42,
		PaymentHash:          "hash",
		Preimage:             "preimage",
		Decision:             "APPROVED",
		NormalizedStatus:     "SUCCEEDED",
		LNDFeeMsat:           sql.NullInt64{Int64: 1200, Valid: true},
		LNDFailureReasonCode: sql.NullInt64{Int64: 0, Valid: true},
	}
	got := KeysendAttemptResult(row)
	if got.PaymentHash != "hash" || got.PaymentPreimage != "preimage" {
		t.Fatalf("unexpected payment identity: %#v", got)
	}
	if got.FeeMsat != 1200 || got.FeeSat != 1.2 {
		t.Fatalf("unexpected fee: %#v", got)
	}
	if got.Message != "already handled" {
		t.Fatalf("unexpected message: %#v", got)
	}
}

func TestRewardAttemptResult(t *testing.T) {
	row := &RewardAttempt{
		RequestID:        "reward-test",
		PaymentHash:      "hash",
		Decision:         "APPROVED",
		NormalizedStatus: "SUCCEEDED",
		PaymentPreimage:  sql.NullString{String: "preimage", Valid: true},
		LNDFeeMsat:       sql.NullInt64{Int64: 900, Valid: true},
	}
	got := RewardAttemptResult(row)
	if got.PaymentHash != "hash" || got.PaymentPreimage != "preimage" {
		t.Fatalf("unexpected payment identity: %#v", got)
	}
	if got.FeeMsat != 900 || got.FeeSat != 0.9 {
		t.Fatalf("unexpected fee: %#v", got)
	}
	if got.Message != "already handled" {
		t.Fatalf("unexpected message: %#v", got)
	}
}

func TestResultFields(t *testing.T) {
	decision, status, txid := resultFields(httpapi.OnchainResponse{
		Decision: "APPROVED",
		Status:   "BROADCASTED",
		TxID:     "abc",
	})
	if decision != "APPROVED" || status != "BROADCASTED" || txid != "abc" {
		t.Fatalf("unexpected fields: %s %s %s", decision, status, txid)
	}
}
