package httpapi

import (
	"encoding/json"
	"testing"
)

func TestLNPayoutRequestAcceptsRoboSatsPayload(t *testing.T) {
	raw := []byte(`{
		"request_id":"lnpayment-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"source":"robosats",
		"kind":"ln_payout",
		"order_id":123,
		"payment_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"payment_request":"lnbc1...",
		"amount_sats":50000,
		"fee_limit_sat":50,
		"timeout_seconds":170,
		"routing_budget_ppm":1000,
		"routing_budget_sats":"50",
		"lnpayment_status":7,
		"order_status":13,
		"receiver_user_id":10,
		"maker_user_id":10,
		"taker_user_id":11,
		"order_type":0,
		"attempt_number":1
	}`)
	var req LNPayoutRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	if req.Source != "robosats" || req.Kind != "ln_payout" || req.OrderID != 123 {
		t.Fatalf("unexpected request: %#v", req)
	}
	if req.ReceiverUserID == nil || *req.ReceiverUserID != 10 {
		t.Fatalf("receiver_user_id not decoded: %#v", req.ReceiverUserID)
	}
}

func TestOnchainPayoutRequestAcceptsRoboSatsPayload(t *testing.T) {
	raw := []byte(`{
		"request_id":"onchainpayment-321",
		"source":"robosats",
		"kind":"onchain_payout",
		"order_id":123,
		"onchain_payment_id":321,
		"address":"bc1qexample",
		"amount_sats":45000,
		"fee_rate_sat_vbyte":5,
		"label":"Payout order #123",
		"spend_unconfirmed":false,
		"gross_payout_sats":50000,
		"mining_fee_sats":1400,
		"swap_fee_rate":"7.2",
		"receiver_user_id":10,
		"receiver_robot_id":99,
		"concept":"PAYBUYER",
		"onchain_payment_status":5,
		"order_status":14,
		"is_swap":true
	}`)
	var req OnchainPayoutRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	if req.Kind != "onchain_payout" || req.OnchainPaymentID != 321 {
		t.Fatalf("unexpected request: %#v", req)
	}
	if req.GrossPayoutSats == nil || *req.GrossPayoutSats != 50000 {
		t.Fatalf("gross payout not decoded: %#v", req.GrossPayoutSats)
	}
}

func TestKeysendRequestAcceptsRoboSatsPayload(t *testing.T) {
	raw := []byte(`{
		"request_id":"keysend-devfund-test",
		"source":"robosats",
		"kind":"devfund_keysend",
		"target_pubkey":"02aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"message":"Devfund donation; x; Order 42: done; 0.1; successful order",
		"amount_sats":1000,
		"fee_limit_sat":5,
		"timeout_seconds":280,
		"sign":false
	}`)
	var req KeysendRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	if req.Kind != "devfund_keysend" || req.RequestID == "" {
		t.Fatalf("unexpected request: %#v", req)
	}
}

func TestRewardSourceRequestAcceptsRoboSatsPayload(t *testing.T) {
	raw := []byte(`{
		"request_id":"reward-source-89895-476229-aaaaaaaa",
		"source":"robosats",
		"kind":"reward_source",
		"source_order_id":89895,
		"receiver_robot_id":476229,
		"receiver_user_id":476315,
		"receiver_robot_name":"HelpfulVeranda735",
		"reward_sats":6289,
		"slashed_bond_payment_hash":"8a08288cd13b6d9ee9bf26fa13f37d968f7e7c281939fc4221c09e4e70a625a0",
		"slashed_bond_sats":12642,
		"slashed_bond_status":2,
		"staked_bond_payment_hash":"49017ac0fcf4595a806cd498f66608689512ad01d6d89b8615932898533a750e",
		"staked_bond_sats":12578,
		"staked_bond_status":3,
		"reason":"slashed_bond"
	}`)
	var req RewardSourceRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	if req.Kind != "reward_source" || req.SourceOrderID != 89895 || req.RewardSats != 6289 {
		t.Fatalf("unexpected request: %#v", req)
	}
}

func TestRewardSourceRequestAcceptsCounterpartyCancellationPayload(t *testing.T) {
	raw := []byte(`{
		"request_id":"reward-source-89895-476229-cancel",
		"source":"robosats",
		"kind":"reward_source",
		"source_order_id":89895,
		"receiver_robot_id":476229,
		"receiver_user_id":476315,
		"receiver_robot_name":"HelpfulVeranda735",
		"reward_sats":5000,
		"slashed_bond_payment_hash":null,
		"slashed_bond_sats":null,
		"slashed_bond_status":null,
		"staked_bond_payment_hash":"49017ac0fcf4595a806cd498f66608689512ad01d6d89b8615932898533a750e",
		"staked_bond_sats":12578,
		"staked_bond_status":3,
		"reason":"counterparty_cancelled"
	}`)
	var req RewardSourceRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	if req.Reason != "counterparty_cancelled" || req.SlashedBondPaymentHash != "" || req.SlashedBondSats != 0 {
		t.Fatalf("unexpected cancellation request: %#v", req)
	}
}

func TestPaymentResponseShape(t *testing.T) {
	code := int64(4)
	resp := PaymentResponse{
		Decision:          "APPROVED",
		Status:            "EXPIRED",
		PaymentHash:       "hash",
		FailureReasonCode: &code,
		Message:           "invoice expired",
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"decision", "status", "payment_hash", "failure_reason_code", "message"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("missing response key %q in %s", key, raw)
		}
	}
}
