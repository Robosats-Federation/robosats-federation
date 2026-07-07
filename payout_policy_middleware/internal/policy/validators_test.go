package policy

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"payout_policy_middleware/internal/config"
	"payout_policy_middleware/internal/httpapi"
	"payout_policy_middleware/internal/lnd"
	"payout_policy_middleware/internal/robosats"
)

type fakeRepo struct {
	lnRow                       *robosats.LNRow
	devfundRow                  *robosats.DevfundOrder
	onchainRow                  *robosats.OnchainRow
	rewardRow                   *robosats.RewardRow
	recentSuccessfulReward      bool
	rewardSources               []robosats.RewardSource
	successfulRewardEarnedTotal int64
	robotIdentity               *robosats.RobotIdentity
	orderBonds                  *robosats.OrderBonds
	bondByHash                  map[string]*robosats.BondInfo
}

func (f fakeRepo) FetchLNRow(context.Context, int64, string) (*robosats.LNRow, error) {
	return f.lnRow, nil
}

func (f fakeRepo) FetchOnchainRow(context.Context, int64, int64) (*robosats.OnchainRow, error) {
	return f.onchainRow, nil
}

func (f fakeRepo) FetchDevfundOrder(context.Context, int64) (*robosats.DevfundOrder, error) {
	return f.devfundRow, nil
}

func (f fakeRepo) FetchRewardRow(context.Context, string) (*robosats.RewardRow, error) {
	return f.rewardRow, nil
}

func (f fakeRepo) HasRecentSuccessfulRewardWithdrawal(context.Context, int64, string, time.Time) (bool, error) {
	return f.recentSuccessfulReward, nil
}

func (f fakeRepo) FetchRobotIdentity(context.Context, int64) (*robosats.RobotIdentity, error) {
	return f.robotIdentity, nil
}

func (f fakeRepo) FetchBondByPaymentHash(_ context.Context, paymentHash string) (*robosats.BondInfo, error) {
	if f.bondByHash != nil {
		return f.bondByHash[paymentHash], nil
	}
	return nil, nil
}

func (f fakeRepo) FetchOrderBonds(context.Context, int64) (*robosats.OrderBonds, error) {
	return f.orderBonds, nil
}

func (f fakeRepo) FetchRewardSources(context.Context, int64) ([]robosats.RewardSource, error) {
	return f.rewardSources, nil
}

func (f fakeRepo) FetchSuccessfulRewardAttemptEarnedTotal(context.Context, int64) (int64, error) {
	return f.successfulRewardEarnedTotal, nil
}

type fakeLND struct {
	decoded        *lnd.DecodedPayReq
	lookupInvoices map[string]*httpapi.LookupInvoiceResponse
	lookupErr      error
}

func (f fakeLND) DecodePayReq(context.Context, string) (*lnd.DecodedPayReq, error) {
	return f.decoded, nil
}

func (f fakeLND) LookupInvoice(_ context.Context, paymentHash string) (*httpapi.LookupInvoiceResponse, error) {
	if f.lookupErr != nil {
		return nil, f.lookupErr
	}
	if f.lookupInvoices == nil {
		return &httpapi.LookupInvoiceResponse{Found: false, State: "NOT_FOUND"}, nil
	}
	if inv, ok := f.lookupInvoices[paymentHash]; ok {
		return inv, nil
	}
	return &httpapi.LookupInvoiceResponse{Found: false, State: "NOT_FOUND"}, nil
}

func settledInvoice() *httpapi.LookupInvoiceResponse {
	return &httpapi.LookupInvoiceResponse{Found: true, State: "SETTLED"}
}

func withSettledEscrow(f fakeLND, hashes ...string) fakeLND {
	if f.lookupInvoices == nil {
		f.lookupInvoices = map[string]*httpapi.LookupInvoiceResponse{}
	}
	for _, h := range hashes {
		f.lookupInvoices[h] = settledInvoice()
	}
	return f
}

func withSettledBond(f fakeLND, hashes ...string) fakeLND {
	return withSettledEscrow(f, hashes...)
}

type fakeBitcoin struct{}

func (fakeBitcoin) ValidateAddress(context.Context, string) error { return nil }

func TestParseDevfundMessage(t *testing.T) {
	orderID, fraction, reason, err := ParseDevfundMessage("Devfund donation; x; Order 42: done; 0.05; successful order")
	if err != nil {
		t.Fatal(err)
	}
	if orderID != 42 || fraction.String() != "0.05" || reason != "successful order" {
		t.Fatalf("unexpected parse result: %d %s %s", orderID, fraction, reason)
	}
}

func TestValidateKeysendRejectsFeeMismatch(t *testing.T) {
	cfg := config.Config{
		MaxDevfundKeysendSats: 5_000_000,
		DevfundPubkey:         "02aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		DevfundKeysendTimeout: 280,
	}
	repo := fakeRepo{
		devfundRow: &robosats.DevfundOrder{OrderID: 42, Proceeds: sql.NullInt64{Int64: 100_000, Valid: true}},
	}
	validator := NewValidator(cfg, repo, repo, fakeLND{}, fakeBitcoin{})

	req := &httpapi.KeysendRequest{
		RequestID:      "keysend-devfund-test",
		Source:         "robosats",
		Kind:           "devfund_keysend",
		TargetPubkey:   cfg.DevfundPubkey,
		Message:        "Devfund donation; x; Order 42: done; 0.10; successful order",
		AmountSats:     10_000,
		FeeLimitSat:    1,
		TimeoutSeconds: 280,
	}
	_, err := validator.ValidateKeysendPolicy(context.Background(), req)
	if err == nil || err.Error() != "keysend fee limit mismatch" {
		t.Fatalf("expected fee mismatch, got %v", err)
	}
}

func TestValidateLNMarksExpiredInvoice(t *testing.T) {
	paymentHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	cfg := config.Config{MaxPayoutSats: 5_000_000}
	receiverID := int64(7)
	row := &robosats.LNRow{
		OrderID:           1,
		OrderStatus:       13,
		OrderType:         0,
		PayoutID:          paymentHash,
		PayoutHash:        paymentHash,
		PayoutStatus:      7,
		PayoutType:        0,
		PayoutConcept:     3,
		PayoutSats:        50_000,
		RoutingBudgetPPM:  1000,
		RoutingBudgetSats: "100",
		PayoutReceiverID:  sql.NullInt64{Int64: receiverID, Valid: true},
		PayoutInvoice:     "lnbc...",
		PayoutExpiresAt:   sql.NullTime{Time: time.Now().Add(-time.Minute), Valid: true},
		EscrowStatus:      2,
		EscrowSats:        50_000,
		TradeEscrowID:     "escrow-hash-1",
		MakerID:           sql.NullInt64{Int64: receiverID, Valid: true},
	}
	repo := fakeRepo{lnRow: row}
	validator := NewValidator(cfg, repo, repo, withSettledEscrow(fakeLND{decoded: &lnd.DecodedPayReq{
		PaymentHash: paymentHash,
		NumSatoshis: 50_000,
		Timestamp:   time.Now().Unix(),
		Expiry:      3600,
	}}, "escrow-hash-1"), fakeBitcoin{})

	got, err := validator.ValidateLNPolicy(context.Background(), httpapi.LNPayoutRequest{
		Source:           "robosats",
		Kind:             "ln_payout",
		OrderID:          1,
		PaymentHash:      paymentHash,
		PaymentRequest:   "lnbc...",
		AmountSats:       50_000,
		FeeLimitSat:      100,
		TimeoutSeconds:   170,
		RoutingBudgetPPM: 1000,
		ReceiverUserID:   &receiverID,
	}, LNOptions{AllowExpired: true})
	if err != nil {
		t.Fatal(err)
	}
	if !got.PolicyInvoiceExpired || got.PolicyInvoiceExpiredReason != "invoice expired in RoboSats DB" {
		t.Fatalf("expected DB expiry marker, got %#v", got)
	}
}

func TestValidateOnchainPolicyAcceptsDecimalMiningFeeRate(t *testing.T) {
	receiverID := int64(7)
	grossPayout := int64(100_000)
	miningFee := int64(574) // int(2.050 * 280), matching RoboSats.
	sentSats := int64(97_426)
	row := &robosats.OnchainRow{
		OrderID:          42,
		OrderStatus:      14,
		OrderType:        0,
		IsSwap:           true,
		PayoutTxID:       321,
		TradeEscrowID:    "escrow",
		MakerID:          sql.NullInt64{Int64: receiverID, Valid: true},
		TakerID:          sql.NullInt64{Int64: 8, Valid: true},
		OnchainPaymentID: 321,
		OnchainConcept:   3,
		OnchainStatus:    5,
		Address:          "bc1qexample",
		NumSatoshis:      grossPayout,
		SentSatoshis:     sentSats,
		MiningFeeRate:    "2.050",
		MiningFeeSats:    miningFee,
		SwapFeeRate:      "2.00",
		PayoutReceiverID: sql.NullInt64{Int64: receiverID, Valid: true},
		EscrowStatus:     2,
		EscrowSats:       grossPayout,
	}
	validator := NewValidator(
		config.Config{MaxOnchainPayoutSats: 5_000_000},
		fakeRepo{onchainRow: row},
		fakeRepo{},
		withSettledEscrow(fakeLND{}, "escrow"),
		fakeBitcoin{},
	)

	_, err := validator.ValidateOnchainPolicy(context.Background(), httpapi.OnchainPayoutRequest{
		Source:           "robosats",
		Kind:             "onchain_payout",
		OrderID:          42,
		OnchainPaymentID: 321,
		Address:          "bc1qexample",
		AmountSats:       sentSats,
		FeeRateSatVByte:  2,
		Label:            "Payout order #42",
		GrossPayoutSats:  &grossPayout,
		MiningFeeSats:    &miningFee,
		ReceiverUserID:   &receiverID,
		Concept:          "PAYBUYER",
		OrderStatus:      14,
		IsSwap:           true,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateRewardSourceAcceptsSlashedBondEvent(t *testing.T) {
	receiverID := int64(99)
	repo := validRewardSourceRepo(receiverID, 10_000, 25_284, 25_284)
	validator := NewValidator(config.Config{MaxRewardWithdrawalSats: 5_000_000}, repo, repo, withSettledBond(fakeLND{}, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), fakeBitcoin{})

	err := validator.ValidateRewardSource(context.Background(), validRewardSourceRequest(receiverID, 10_000, 25_284, 25_284))
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateRewardSourceRejectsSlashedBondMissingInLND(t *testing.T) {
	receiverID := int64(99)
	repo := validRewardSourceRepo(receiverID, 10_000, 25_284, 25_284)
	validator := NewValidator(config.Config{MaxRewardWithdrawalSats: 5_000_000}, repo, repo, fakeLND{}, fakeBitcoin{})

	err := validator.ValidateRewardSource(context.Background(), validRewardSourceRequest(receiverID, 10_000, 25_284, 25_284))
	if err == nil || err.Error() != "bond hold invoice not found in LND" {
		t.Fatalf("expected slashed bond LND lookup error, got %v", err)
	}
}

func TestValidateRewardSourceRejectsOversizedReward(t *testing.T) {
	receiverID := int64(99)
	repo := validRewardSourceRepo(receiverID, 10_000, 19_900, 19_900)
	validator := NewValidator(config.Config{MaxRewardWithdrawalSats: 5_000_000}, repo, repo, fakeLND{}, fakeBitcoin{})

	err := validator.ValidateRewardSource(context.Background(), validRewardSourceRequest(receiverID, 10_000, 19_900, 19_900))
	if err == nil || err.Error() != "reward exceeds slashed bond policy" {
		t.Fatalf("expected source cap error, got %v", err)
	}
}

func TestValidateRewardSourceAcceptsCounterpartyCancellation(t *testing.T) {
	receiverID := int64(99)
	repo := validCounterpartyCancellationRewardSourceRepo(receiverID, 5_000, 12_000)
	validator := NewValidator(config.Config{MaxRewardWithdrawalSats: 5_000_000}, repo, repo, fakeLND{}, fakeBitcoin{})

	err := validator.ValidateRewardSource(context.Background(), validCounterpartyCancellationRewardSourceRequest(receiverID, 5_000, 12_000))
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateRewardSourceRejectsCancellationWithPeerBond(t *testing.T) {
	receiverID := int64(99)
	repo := validCounterpartyCancellationRewardSourceRepo(receiverID, 5_000, 12_000)
	repo.orderBonds.TakerBondID = sql.NullString{String: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Valid: true}
	validator := NewValidator(config.Config{MaxRewardWithdrawalSats: 5_000_000}, repo, repo, fakeLND{}, fakeBitcoin{})

	err := validator.ValidateRewardSource(context.Background(), validCounterpartyCancellationRewardSourceRequest(receiverID, 5_000, 12_000))
	if err == nil || err.Error() != "counterparty cancellation reward requires missing peer bond" {
		t.Fatalf("expected peer bond error, got %v", err)
	}
}

func TestValidateRewardWithdrawalUsesStructuredRewardSources(t *testing.T) {
	paymentHash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	receiverID := int64(99)
	row := validRewardRow(paymentHash, receiverID, 10_000, 9_900, 1000, "100")
	repo := validRewardSourceRepo(receiverID, 10_000, 25_284, 25_284)
	repo.rewardRow = row
	validator := NewValidator(
		config.Config{MaxRewardWithdrawalSats: 5_000_000},
		repo,
		repo,
		fakeLND{decoded: &lnd.DecodedPayReq{
			PaymentHash: paymentHash,
			NumSatoshis: 9_900,
			Timestamp:   time.Now().Unix(),
			Expiry:      3600,
		}},
		fakeBitcoin{},
	)

	got, err := validator.ValidateRewardWithdrawal(context.Background(), validRewardWithdrawalRequest(paymentHash, receiverID, 9_900, 100, 1000, "100"))
	if err != nil {
		t.Fatal(err)
	}
	if got.PayoutHash != paymentHash || got.SourceOrderID != 89895 {
		t.Fatalf("unexpected reward row: %#v", got)
	}
}

func TestValidateRewardWithdrawalRejectsMissingStructuredSource(t *testing.T) {
	paymentHash := "1111111111111111111111111111111111111111111111111111111111111111"
	receiverID := int64(99)
	row := validRewardRow(paymentHash, receiverID, 10_000, 10_000, 0, "0")
	repo := fakeRepo{rewardRow: row}
	validator := NewValidator(
		config.Config{MaxRewardWithdrawalSats: 5_000_000},
		repo,
		repo,
		fakeLND{decoded: &lnd.DecodedPayReq{
			PaymentHash: paymentHash,
			NumSatoshis: 10_000,
			Timestamp:   time.Now().Unix(),
			Expiry:      3600,
		}},
		fakeBitcoin{},
	)

	_, err := validator.ValidateRewardWithdrawal(context.Background(), validRewardWithdrawalRequest(paymentHash, receiverID, 10_000, 10, 0, "0"))
	if err == nil || err.Error() != "structured reward source not found" {
		t.Fatalf("expected missing source error, got %v", err)
	}
}

func TestValidateRewardWithdrawalAllowsCoordinatorPaidRoutingFee(t *testing.T) {
	paymentHash := "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	receiverID := int64(99)
	row := validRewardRow(paymentHash, receiverID, 10_000, 10_000, 0, "0")
	repo := validRewardSourceRepo(receiverID, 10_000, 25_284, 25_284)
	repo.rewardRow = row
	validator := NewValidator(
		config.Config{MaxRewardWithdrawalSats: 5_000_000},
		repo,
		repo,
		fakeLND{decoded: &lnd.DecodedPayReq{
			PaymentHash: paymentHash,
			NumSatoshis: 10_000,
			Timestamp:   time.Now().Unix(),
			Expiry:      3600,
		}},
		fakeBitcoin{},
	)

	_, err := validator.ValidateRewardWithdrawal(context.Background(), validRewardWithdrawalRequest(paymentHash, receiverID, 10_000, 10, 0, "0"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateRewardWithdrawalRejectsRecentSuccessfulWithdrawal(t *testing.T) {
	paymentHash := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	receiverID := int64(99)
	row := validRewardRow(paymentHash, receiverID, 10_000, 10_000, 0, "0")
	repo := validRewardSourceRepo(receiverID, 10_000, 25_284, 25_284)
	repo.rewardRow = row
	repo.recentSuccessfulReward = true
	validator := NewValidator(
		config.Config{MaxRewardWithdrawalSats: 5_000_000, RewardWithdrawalCooldownHours: 48},
		repo,
		repo,
		fakeLND{decoded: &lnd.DecodedPayReq{
			PaymentHash: paymentHash,
			NumSatoshis: 10_000,
			Timestamp:   time.Now().Unix(),
			Expiry:      3600,
		}},
		fakeBitcoin{},
	)

	_, err := validator.ValidateRewardWithdrawal(context.Background(), validRewardWithdrawalRequest(paymentHash, receiverID, 10_000, 10, 0, "0"))
	if err == nil || err.Error() != "reward withdrawal is temporarily unavailable" {
		t.Fatalf("expected cooldown error, got %v", err)
	}
}

func TestValidateRewardWithdrawalSubtractsPreviouslyClaimedStructuredRewards(t *testing.T) {
	paymentHash := "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	receiverID := int64(99)
	row := validRewardRow(paymentHash, receiverID, 10_000, 9_900, 1000, "100")
	repo := validRewardSourceRepo(receiverID, 10_000, 25_284, 25_284)
	repo.rewardRow = row
	repo.successfulRewardEarnedTotal = 10_000
	validator := NewValidator(
		config.Config{MaxRewardWithdrawalSats: 5_000_000},
		repo,
		repo,
		fakeLND{decoded: &lnd.DecodedPayReq{
			PaymentHash: paymentHash,
			NumSatoshis: 9_900,
			Timestamp:   time.Now().Unix(),
			Expiry:      3600,
		}},
		fakeBitcoin{},
	)

	_, err := validator.ValidateRewardWithdrawal(context.Background(), validRewardWithdrawalRequest(paymentHash, receiverID, 9_900, 100, 1000, "100"))
	if err == nil || err.Error() != "reward amount exceeds structured reward balance" {
		t.Fatalf("expected structured balance error, got %v", err)
	}
}

func TestValidateRewardWithdrawalUsesCounterpartyCancellationSource(t *testing.T) {
	paymentHash := "9999999999999999999999999999999999999999999999999999999999999999"
	receiverID := int64(99)
	row := validRewardRow(paymentHash, receiverID, 5_000, 5_000, 0, "0")
	repo := validCounterpartyCancellationRewardSourceRepo(receiverID, 5_000, 12_000)
	repo.rewardRow = row
	validator := NewValidator(
		config.Config{MaxRewardWithdrawalSats: 5_000_000},
		repo,
		repo,
		fakeLND{decoded: &lnd.DecodedPayReq{
			PaymentHash: paymentHash,
			NumSatoshis: 5_000,
			Timestamp:   time.Now().Unix(),
			Expiry:      3600,
		}},
		fakeBitcoin{},
	)

	_, err := validator.ValidateRewardWithdrawal(context.Background(), validRewardWithdrawalRequest(paymentHash, receiverID, 5_000, 5, 0, "0"))
	if err != nil {
		t.Fatal(err)
	}
}

func validRewardRow(paymentHash string, receiverID int64, earned int64, payoutSats int64, routingPPM int64, routingSats string) *robosats.RewardRow {
	return &robosats.RewardRow{
		ReceiverUserID:    sql.NullInt64{Int64: receiverID, Valid: true},
		ReceiverRobotID:   476229,
		ReceiverUsername:  "HelpfulVeranda735",
		EarnedRewards:     earned,
		PayoutHash:        paymentHash,
		PayoutInvoice:     "lnbc-reward",
		PayoutSats:        payoutSats,
		PayoutExpiresAt:   sql.NullTime{Time: time.Now().Add(time.Hour), Valid: true},
		RoutingBudgetPPM:  routingPPM,
		RoutingBudgetSats: routingSats,
	}
}

func validRewardWithdrawalRequest(paymentHash string, receiverID int64, amountSats int64, feeLimitSat int64, routingPPM int64, routingSats string) httpapi.RewardWithdrawalRequest {
	return httpapi.RewardWithdrawalRequest{
		RequestID:         "reward-" + paymentHash,
		Source:            "robosats",
		Kind:              "reward_withdrawal",
		PaymentHash:       paymentHash,
		PaymentRequest:    "lnbc-reward",
		AmountSats:        amountSats,
		FeeLimitSat:       feeLimitSat,
		TimeoutSeconds:    170,
		RoutingBudgetPPM:  routingPPM,
		RoutingBudgetSats: routingSats,
		ReceiverUserID:    &receiverID,
		ReceiverRobotName: "HelpfulVeranda735",
	}
}

func validRewardSourceRequest(receiverID int64, rewardSats int64, slashedBondSats int64, stakedBondSats int64) httpapi.RewardSourceRequest {
	return httpapi.RewardSourceRequest{
		RequestID:              "reward-source-89895-476229",
		Source:                 "robosats",
		Kind:                   "reward_source",
		SourceOrderID:          89895,
		ReceiverRobotID:        476229,
		ReceiverUserID:         receiverID,
		ReceiverRobotName:      "HelpfulVeranda735",
		RewardSats:             rewardSats,
		SlashedBondPaymentHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SlashedBondSats:        slashedBondSats,
		SlashedBondStatus:      2,
		StakedBondPaymentHash:  "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		StakedBondSats:         stakedBondSats,
		StakedBondStatus:       3,
		Reason:                 "slashed_bond",
	}
}

func validRewardSourceRepo(receiverID int64, rewardSats int64, slashedBondSats int64, stakedBondSats int64) fakeRepo {
	source := validRewardSourceRequest(receiverID, rewardSats, slashedBondSats, stakedBondSats)
	return fakeRepo{
		robotIdentity: &robosats.RobotIdentity{
			UserID:   receiverID,
			RobotID:  476229,
			Username: "HelpfulVeranda735",
		},
		orderBonds: &robosats.OrderBonds{
			OrderID:     89895,
			MakerID:     sql.NullInt64{Int64: 7, Valid: true},
			TakerID:     sql.NullInt64{Int64: receiverID, Valid: true},
			MakerBondID: sql.NullString{String: source.SlashedBondPaymentHash, Valid: true},
			TakerBondID: sql.NullString{String: source.StakedBondPaymentHash, Valid: true},
		},
		bondByHash: map[string]*robosats.BondInfo{
			source.SlashedBondPaymentHash: {
				PaymentHash: source.SlashedBondPaymentHash,
				NumSatoshis: slashedBondSats,
				Status:      2,
				Concept:     0,
				SenderID:    sql.NullInt64{Int64: 7, Valid: true},
			},
			source.StakedBondPaymentHash: {
				PaymentHash: source.StakedBondPaymentHash,
				NumSatoshis: stakedBondSats,
				Status:      3,
				Concept:     1,
				SenderID:    sql.NullInt64{Int64: receiverID, Valid: true},
			},
		},
		rewardSources: []robosats.RewardSource{
			{
				SourceOrderID:          source.SourceOrderID,
				ReceiverRobotID:        source.ReceiverRobotID,
				ReceiverUserID:         source.ReceiverUserID,
				ReceiverRobotName:      source.ReceiverRobotName,
				RewardSats:             source.RewardSats,
				SlashedBondPaymentHash: source.SlashedBondPaymentHash,
				SlashedBondSats:        source.SlashedBondSats,
				SlashedBondStatus:      source.SlashedBondStatus,
				StakedBondPaymentHash:  source.StakedBondPaymentHash,
				StakedBondSats:         source.StakedBondSats,
				StakedBondStatus:       source.StakedBondStatus,
				Reason:                 source.Reason,
			},
		},
	}
}

func validCounterpartyCancellationRewardSourceRequest(receiverID int64, rewardSats int64, stakedBondSats int64) httpapi.RewardSourceRequest {
	return httpapi.RewardSourceRequest{
		RequestID:              "reward-source-89895-476229-cancel",
		Source:                 "robosats",
		Kind:                   "reward_source",
		SourceOrderID:          89895,
		ReceiverRobotID:        476229,
		ReceiverUserID:         receiverID,
		ReceiverRobotName:      "HelpfulVeranda735",
		RewardSats:             rewardSats,
		SlashedBondPaymentHash: "",
		SlashedBondSats:        0,
		SlashedBondStatus:      0,
		StakedBondPaymentHash:  "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		StakedBondSats:         stakedBondSats,
		StakedBondStatus:       3,
		Reason:                 "counterparty_cancelled",
	}
}

func validCounterpartyCancellationRewardSourceRepo(receiverID int64, rewardSats int64, stakedBondSats int64) fakeRepo {
	source := validCounterpartyCancellationRewardSourceRequest(receiverID, rewardSats, stakedBondSats)
	return fakeRepo{
		robotIdentity: &robosats.RobotIdentity{
			UserID:   receiverID,
			RobotID:  476229,
			Username: "HelpfulVeranda735",
		},
		orderBonds: &robosats.OrderBonds{
			OrderID:     89895,
			MakerID:     sql.NullInt64{Int64: receiverID, Valid: true},
			TakerID:     sql.NullInt64{},
			MakerBondID: sql.NullString{String: source.StakedBondPaymentHash, Valid: true},
			TakerBondID: sql.NullString{},
		},
		bondByHash: map[string]*robosats.BondInfo{
			source.StakedBondPaymentHash: {
				PaymentHash: source.StakedBondPaymentHash,
				NumSatoshis: stakedBondSats,
				Status:      3,
				Concept:     0,
				SenderID:    sql.NullInt64{Int64: receiverID, Valid: true},
			},
		},
		rewardSources: []robosats.RewardSource{
			{
				SourceOrderID:          source.SourceOrderID,
				ReceiverRobotID:        source.ReceiverRobotID,
				ReceiverUserID:         source.ReceiverUserID,
				ReceiverRobotName:      source.ReceiverRobotName,
				RewardSats:             source.RewardSats,
				SlashedBondPaymentHash: source.SlashedBondPaymentHash,
				SlashedBondSats:        source.SlashedBondSats,
				SlashedBondStatus:      source.SlashedBondStatus,
				StakedBondPaymentHash:  source.StakedBondPaymentHash,
				StakedBondSats:         source.StakedBondSats,
				StakedBondStatus:       source.StakedBondStatus,
				Reason:                 source.Reason,
			},
		},
	}
}
