package policy

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"payout_policy_middleware/internal/config"
	"payout_policy_middleware/internal/httpapi"
	"payout_policy_middleware/internal/lnd"
	"payout_policy_middleware/internal/robosats"
)

type Repository interface {
	FetchLNRow(ctx context.Context, orderID int64, paymentHash string) (*robosats.LNRow, error)
	FetchOnchainRow(ctx context.Context, orderID int64, onchainPaymentID int64) (*robosats.OnchainRow, error)
	FetchDevfundOrder(ctx context.Context, orderID int64) (*robosats.DevfundOrder, error)
	FetchRewardRow(ctx context.Context, paymentHash string) (*robosats.RewardRow, error)
	HasRecentSuccessfulRewardWithdrawal(ctx context.Context, receiverUserID int64, currentPaymentHash string, since time.Time) (bool, error)
	FetchRobotIdentity(ctx context.Context, userID int64) (*robosats.RobotIdentity, error)
	FetchBondByPaymentHash(ctx context.Context, paymentHash string) (*robosats.BondInfo, error)
	FetchOrderBonds(ctx context.Context, orderID int64) (*robosats.OrderBonds, error)
}

type RewardSourceRepository interface {
	FetchRewardSources(ctx context.Context, receiverUserID int64) ([]robosats.RewardSource, error)
	FetchSuccessfulRewardAttemptEarnedTotal(ctx context.Context, receiverUserID int64) (int64, error)
}

type LNDDecoder interface {
	DecodePayReq(ctx context.Context, paymentRequest string) (*lnd.DecodedPayReq, error)
	LookupInvoice(ctx context.Context, paymentHash string) (*httpapi.LookupInvoiceResponse, error)
}

type BitcoinValidator interface {
	ValidateAddress(ctx context.Context, address string) error
}

type Validator struct {
	cfg           config.Config
	repo          Repository
	rewardSources RewardSourceRepository
	lnd           LNDDecoder
	bitcoin       BitcoinValidator
}

type LNOptions struct {
	AllowTerminal bool
	AllowExpired  bool
}

func NewValidator(cfg config.Config, repo Repository, rewardSources RewardSourceRepository, lnd LNDDecoder, bitcoin BitcoinValidator) *Validator {
	return &Validator{cfg: cfg, repo: repo, rewardSources: rewardSources, lnd: lnd, bitcoin: bitcoin}
}

func (v *Validator) ValidateLNPolicy(ctx context.Context, req httpapi.LNPayoutRequest, opts LNOptions) (*robosats.LNRow, error) {
	if req.Source != "robosats" {
		return nil, NewError("source must be robosats")
	}
	if req.Kind != "" && req.Kind != "ln_payout" {
		return nil, NewError("kind must be ln_payout")
	}
	if req.PaymentHash == "" {
		return nil, NewError("payment_hash is required")
	}
	if len(req.PaymentHash) != 64 {
		return nil, NewError("payment_hash must be 32-byte hex")
	}
	if _, err := hex.DecodeString(req.PaymentHash); err != nil {
		return nil, NewError("payment_hash must be hex")
	}
	if req.PaymentRequest == "" {
		return nil, NewError("payment_request is required")
	}
	if req.AmountSats <= 0 || req.AmountSats >= v.cfg.MaxPayoutSats {
		return nil, NewError("amount is outside policy")
	}
	if req.FeeLimitSat < 0 {
		return nil, NewError("fee_limit_sat must not be negative")
	}
	if req.TimeoutSeconds < 1 || req.TimeoutSeconds > 300 {
		return nil, NewError("timeout_seconds is outside policy")
	}

	row, err := v.repo.FetchLNRow(ctx, req.OrderID, req.PaymentHash)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, NewError("order payout row not found")
	}
	if row.PayoutID != req.PaymentHash {
		return nil, NewError("order payout_id mismatch")
	}
	if row.PayoutInvoice != req.PaymentRequest {
		return nil, NewError("invoice mismatch")
	}
	if row.PayoutSats != req.AmountSats {
		return nil, NewError("amount mismatch")
	}
	if row.PayoutConcept != 3 {
		return nil, NewError("payout concept is not PAYBUYER")
	}
	if row.PayoutType != 0 {
		return nil, NewError("payout type is not normal invoice")
	}
	if row.IsSwap {
		return nil, NewError("order is on-chain swap")
	}
	terminalRow := opts.AllowTerminal && (row.OrderStatus == 14 || row.PayoutStatus == 8)
	if !containsInt64([]int64{13, 15, 14}, row.OrderStatus) || (!opts.AllowTerminal && row.OrderStatus == 14) {
		return nil, NewError("order is not in payout/retry state")
	}
	if !containsInt64([]int64{7, 9, 8}, row.PayoutStatus) || (!opts.AllowTerminal && row.PayoutStatus == 8) {
		return nil, NewError("ln payout is not in flight/retry state")
	}
	if row.EscrowStatus != 2 {
		return nil, NewError("escrow is not settled")
	}
	if row.EscrowSats < req.AmountSats {
		return nil, NewError("escrow is smaller than payout")
	}
	if err := v.verifyEscrowSettledViaLND(ctx, row.TradeEscrowID); err != nil {
		return nil, err
	}
	routingBudget, err := decimal.NewFromString(row.RoutingBudgetSats)
	if err != nil {
		return nil, err
	}
	if decimal.NewFromInt(req.FeeLimitSat).GreaterThan(routingBudget) {
		return nil, NewError("fee limit exceeds routing budget")
	}
	if req.RoutingBudgetPPM != row.RoutingBudgetPPM {
		return nil, NewError("routing budget ppm mismatch")
	}
	if !sameNullableID(req.ReceiverUserID, row.PayoutReceiverID) {
		return nil, NewError("receiver mismatch")
	}
	buyer, err := robosats.BuyerUserID(row.OrderType, row.MakerID, row.TakerID)
	if err != nil {
		return nil, NewError(err.Error())
	}
	if !nullableInt64Equal(row.PayoutReceiverID, buyer) {
		return nil, NewError("payout receiver is not buyer")
	}
	decoded, err := v.lnd.DecodePayReq(ctx, req.PaymentRequest)
	if err != nil {
		return nil, err
	}
	if decoded.PaymentHash != req.PaymentHash {
		return nil, NewError("decoded invoice hash mismatch")
	}
	if decoded.NumSatoshis != req.AmountSats {
		return nil, NewError("decoded invoice amount mismatch")
	}
	if err := v.verifyBondsNotSettledViaLND(ctx, row.MakerBondID, row.TakerBondID); err != nil {
		return nil, err
	}
	if terminalRow {
		return row, nil
	}
	dbExpired := isPast(row.PayoutExpiresAt)
	decodedExpiry := time.Unix(decoded.Timestamp+decoded.Expiry, 0).UTC()
	decodedExpired := decodedExpiry.Before(time.Now().UTC())
	if dbExpired || decodedExpired {
		reason := "decoded invoice expired"
		if dbExpired {
			reason = "invoice expired in RoboSats DB"
		}
		if opts.AllowExpired {
			row.PolicyInvoiceExpired = true
			row.PolicyInvoiceExpiredReason = reason
			return row, nil
		}
		return nil, NewError(reason)
	}
	return row, nil
}

func (v *Validator) ValidateKeysendPolicy(ctx context.Context, req *httpapi.KeysendRequest) (*robosats.DevfundOrder, error) {
	if req.Source != "robosats" {
		return nil, NewError("source must be robosats")
	}
	if req.Kind != "devfund_keysend" {
		return nil, NewError("kind must be devfund_keysend")
	}
	if req.RequestID == "" {
		return nil, NewError("request_id is required")
	}
	req.TargetPubkey = strings.ToLower(req.TargetPubkey)
	if req.TargetPubkey == "" {
		return nil, NewError("target_pubkey is required")
	}
	if req.Message == "" {
		return nil, NewError("message is required")
	}
	if v.cfg.DevfundPubkey == "" {
		return nil, NewError("DEVFUND_PUBKEY is not configured")
	}
	if err := validatePubkey(req.TargetPubkey); err != nil {
		return nil, err
	}
	if err := validatePubkey(v.cfg.DevfundPubkey); err != nil {
		return nil, err
	}
	if req.TargetPubkey != v.cfg.DevfundPubkey {
		return nil, NewError("target_pubkey does not match configured devfund pubkey")
	}
	if len(req.Message) > 500 {
		return nil, NewError("devfund message is too long")
	}
	if req.AmountSats <= 0 || req.AmountSats >= v.cfg.MaxDevfundKeysendSats {
		return nil, NewError("amount is outside keysend policy")
	}
	expectedFee := decimal.Max(decimal.NewFromInt(5), decimal.NewFromInt(req.AmountSats).Mul(decimal.RequireFromString("0.0001"))).IntPart()
	if req.FeeLimitSat != expectedFee {
		return nil, NewError("keysend fee limit mismatch")
	}
	if req.TimeoutSeconds != v.cfg.DevfundKeysendTimeout {
		return nil, NewError("keysend timeout mismatch")
	}
	if req.Sign {
		return nil, NewError("signed keysend is not supported by policy")
	}
	orderID, fraction, _, err := ParseDevfundMessage(req.Message)
	if err != nil {
		return nil, err
	}
	if fraction.LessThan(decimal.Zero) || fraction.GreaterThan(decimal.NewFromInt(1)) {
		return nil, NewError("devfund donation fraction is outside policy")
	}
	row, err := v.repo.FetchDevfundOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, NewError("devfund order not found")
	}
	proceeds := int64(0)
	if row.Proceeds.Valid {
		proceeds = row.Proceeds.Int64
	}
	if proceeds < req.AmountSats {
		return nil, NewError("order proceeds are smaller than keysend amount")
	}
	if decimal.NewFromInt(req.AmountSats).GreaterThan(decimal.NewFromInt(proceeds).Mul(fraction)) {
		return nil, NewError("keysend amount exceeds proceeds fraction")
	}
	return row, nil
}

func (v *Validator) ValidateOnchainPolicy(ctx context.Context, req httpapi.OnchainPayoutRequest) (*robosats.OnchainRow, error) {
	if req.Source != "robosats" {
		return nil, NewError("source must be robosats")
	}
	if req.Kind != "onchain_payout" {
		return nil, NewError("kind must be onchain_payout")
	}
	if req.Address == "" {
		return nil, NewError("address is required")
	}
	if req.Label == "" {
		return nil, NewError("label is required")
	}
	if req.AmountSats <= 0 || req.AmountSats >= v.cfg.MaxOnchainPayoutSats {
		return nil, NewError("amount is outside policy")
	}
	if req.FeeRateSatVByte < 1 || req.FeeRateSatVByte > 500 {
		return nil, NewError("fee rate is outside policy")
	}
	if req.SpendUnconfirmed && !v.cfg.AllowSpendUnconfirmed {
		return nil, NewError("spend_unconfirmed is disabled")
	}
	if req.Label != fmt.Sprintf("Payout order #%d", req.OrderID) {
		return nil, NewError("label mismatch")
	}
	if err := v.bitcoin.ValidateAddress(ctx, req.Address); err != nil {
		return nil, err
	}
	row, err := v.repo.FetchOnchainRow(ctx, req.OrderID, req.OnchainPaymentID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, NewError("on-chain payout row not found")
	}
	if row.PayoutTxID != req.OnchainPaymentID {
		return nil, NewError("order payout_tx_id mismatch")
	}
	if !row.IsSwap {
		return nil, NewError("order is not on-chain swap")
	}
	if row.OrderStatus != 14 {
		return nil, NewError("order is not successful/queued for on-chain payout")
	}
	if row.OnchainStatus != 5 {
		return nil, NewError("on-chain payout is not queued")
	}
	if row.Broadcasted || (row.TxID.Valid && row.TxID.String != "") {
		return nil, NewError("on-chain payout already broadcasted")
	}
	if row.OnchainConcept != 3 {
		return nil, NewError("on-chain concept is not PAYBUYER")
	}
	if row.EscrowStatus != 2 {
		return nil, NewError("escrow is not settled")
	}
	if row.EscrowSats < row.NumSatoshis {
		return nil, NewError("escrow is smaller than gross payout")
	}
	if err := v.verifyEscrowSettledViaLND(ctx, row.TradeEscrowID); err != nil {
		return nil, err
	}
	if row.Address != req.Address {
		return nil, NewError("address mismatch")
	}
	if row.SentSatoshis != req.AmountSats {
		return nil, NewError("sent amount mismatch")
	}
	miningFeeRate, err := decimal.NewFromString(row.MiningFeeRate)
	if err != nil {
		return nil, err
	}
	if miningFeeRate.IntPart() != req.FeeRateSatVByte {
		return nil, NewError("fee rate mismatch")
	}
	if req.GrossPayoutSats != nil && *req.GrossPayoutSats != row.NumSatoshis {
		return nil, NewError("gross payout mismatch")
	}
	if req.MiningFeeSats != nil && *req.MiningFeeSats != row.MiningFeeSats {
		return nil, NewError("mining fee mismatch")
	}
	if req.AmountSats < 20_000 {
		return nil, NewError("amount is below dust floor")
	}
	buyer, err := robosats.BuyerUserID(row.OrderType, row.MakerID, row.TakerID)
	if err != nil {
		return nil, NewError(err.Error())
	}
	if !nullableInt64Equal(row.PayoutReceiverID, buyer) {
		return nil, NewError("payout receiver is not buyer")
	}
	expectedMiningFee := miningFeeRate.Mul(decimal.NewFromInt(280)).IntPart()
	if absInt64(row.MiningFeeSats-expectedMiningFee) > 1 {
		return nil, NewError("mining fee formula mismatch")
	}
	swapFee, err := decimal.NewFromString(row.SwapFeeRate)
	if err != nil {
		return nil, err
	}
	expectedSent := decimal.NewFromInt(row.NumSatoshis).
		Sub(decimal.NewFromInt(row.NumSatoshis).Mul(swapFee).Div(decimal.NewFromInt(100))).
		Sub(decimal.NewFromInt(row.MiningFeeSats)).
		IntPart()
	if absInt64(row.SentSatoshis-expectedSent) > 1 {
		return nil, NewError("sent amount formula mismatch")
	}
	return row, nil
}

func ParseDevfundMessage(message string) (int64, decimal.Decimal, string, error) {
	parts := strings.Split(message, ";")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	if len(parts) < 5 || parts[0] != "Devfund donation" {
		return 0, decimal.Zero, "", NewError("devfund message format mismatch")
	}
	match := regexp.MustCompile(`\bOrder\s+(\d+):`).FindStringSubmatch(parts[2])
	if len(match) != 2 {
		return 0, decimal.Zero, "", NewError("devfund message order id missing")
	}
	var orderID int64
	if _, err := fmt.Sscan(match[1], &orderID); err != nil {
		return 0, decimal.Zero, "", NewError("devfund message order id missing")
	}
	fraction, err := decimal.NewFromString(parts[3])
	if err != nil {
		return 0, decimal.Zero, "", NewError("devfund donation fraction is invalid")
	}
	reason := parts[4]
	if reason != "successful order" && reason != "slashed bond" {
		return 0, decimal.Zero, "", NewError("devfund reason is not allowed")
	}
	return orderID, fraction, reason, nil
}

func validatePubkey(pubkey string) error {
	if len(pubkey) != 66 || (pubkey[:2] != "02" && pubkey[:2] != "03") {
		return NewError("target_pubkey must be a compressed secp256k1 pubkey")
	}
	if _, err := hex.DecodeString(pubkey); err != nil {
		return NewError("target_pubkey must be hex")
	}
	return nil
}

func isPast(value sql.NullTime) bool {
	return value.Valid && value.Time.Before(time.Now().UTC())
}

func sameNullableID(value *int64, dbValue sql.NullInt64) bool {
	if value == nil {
		return !dbValue.Valid
	}
	return dbValue.Valid && *value == dbValue.Int64
}

func nullableInt64Equal(a, b sql.NullInt64) bool {
	if a.Valid != b.Valid {
		return false
	}
	return !a.Valid || a.Int64 == b.Int64
}

func containsInt64(values []int64, target int64) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func absInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func (v *Validator) ValidateRewardWithdrawal(ctx context.Context, req httpapi.RewardWithdrawalRequest) (*robosats.RewardRow, error) {
	if req.Source != "robosats" {
		return nil, NewError("source must be robosats")
	}
	if req.Kind != "" && req.Kind != "reward_withdrawal" {
		return nil, NewError("kind must be reward_withdrawal")
	}
	req.RequestID = strings.TrimSpace(req.RequestID)
	if req.RequestID == "" {
		return nil, NewError("request_id is required")
	}
	if req.PaymentHash == "" {
		return nil, NewError("payment_hash is required")
	}
	if len(req.PaymentHash) != 64 {
		return nil, NewError("payment_hash must be 32-byte hex")
	}
	if _, err := hex.DecodeString(req.PaymentHash); err != nil {
		return nil, NewError("payment_hash must be hex")
	}
	if req.PaymentRequest == "" {
		return nil, NewError("payment_request is required")
	}
	req.ReceiverRobotName = strings.TrimSpace(req.ReceiverRobotName)
	if req.ReceiverRobotName == "" {
		return nil, NewError("receiver_robot_name is required")
	}
	if req.AmountSats <= 0 || req.AmountSats >= v.cfg.MaxRewardWithdrawalSats {
		return nil, NewError("reward amount is outside policy")
	}
	if req.FeeLimitSat < 0 {
		return nil, NewError("fee_limit_sat must not be negative")
	}
	if req.TimeoutSeconds < 1 || req.TimeoutSeconds > 300 {
		return nil, NewError("timeout_seconds is outside policy")
	}

	row, err := v.repo.FetchRewardRow(ctx, req.PaymentHash)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, NewError("reward withdrawal row not found")
	}
	if row.PayoutHash != req.PaymentHash {
		return nil, NewError("reward payment hash mismatch")
	}
	if row.PayoutInvoice != req.PaymentRequest {
		return nil, NewError("reward invoice mismatch")
	}
	if row.PayoutSats != req.AmountSats {
		return nil, NewError("reward amount mismatch")
	}
	if !sameNullableID(req.ReceiverUserID, row.ReceiverUserID) {
		return nil, NewError("reward receiver mismatch")
	}
	if row.ReceiverUsername != req.ReceiverRobotName {
		return nil, NewError("reward receiver robot name mismatch")
	}
	if req.RoutingBudgetPPM != row.RoutingBudgetPPM {
		return nil, NewError("reward routing budget ppm mismatch")
	}
	routingBudget, err := decimal.NewFromString(row.RoutingBudgetSats)
	if err != nil {
		return nil, err
	}
	requestRoutingBudget, err := decimal.NewFromString(req.RoutingBudgetSats)
	if err != nil {
		return nil, NewError("reward routing budget sats is invalid")
	}
	if !requestRoutingBudget.Equal(routingBudget) {
		return nil, NewError("reward routing budget sats mismatch")
	}
	if routingBudget.IsZero() {
		if req.FeeLimitSat > rewardCoordinatorFeeLimit(req.AmountSats, v.cfg) {
			return nil, NewError("reward fee limit exceeds coordinator reward fee policy")
		}
	} else if decimal.NewFromInt(req.FeeLimitSat).GreaterThan(routingBudget) {
		return nil, NewError("reward fee limit exceeds user-funded routing budget")
	}

	// Check earned_rewards >= amount_sats
	if row.EarnedRewards < req.AmountSats {
		return nil, NewError("reward amount exceeds earned rewards balance")
	}
	if !row.ReceiverUserID.Valid {
		return nil, NewError("reward receiver is missing")
	}
	cooldownSince := time.Now().UTC().Add(-rewardWithdrawalCooldown(v.cfg))
	hasRecentRewardWithdrawal, err := v.repo.HasRecentSuccessfulRewardWithdrawal(ctx, row.ReceiverUserID.Int64, req.PaymentHash, cooldownSince)
	if err != nil {
		return nil, err
	}
	if hasRecentRewardWithdrawal {
		return nil, NewError("reward withdrawal is temporarily unavailable")
	}

	if v.rewardSources == nil {
		return nil, fmt.Errorf("reward source repository is not configured")
	}
	sources, err := v.rewardSources.FetchRewardSources(ctx, row.ReceiverUserID.Int64)
	if err != nil {
		return nil, err
	}
	sourceTotal := int64(0)
	sourceOrderID := int64(0)
	for _, source := range sources {
		if source.ReceiverUserID != row.ReceiverUserID.Int64 {
			continue
		}
		if source.ReceiverRobotID != row.ReceiverRobotID {
			continue
		}
		if source.ReceiverRobotName != req.ReceiverRobotName {
			continue
		}
		if !supportedRewardSourceReason(source.Reason) {
			continue
		}
		if source.RewardSats <= 0 {
			continue
		}
		sourceTotal += source.RewardSats
		if sourceOrderID == 0 {
			sourceOrderID = source.SourceOrderID
		}
	}
	if sourceTotal <= 0 {
		return nil, NewError("structured reward source not found")
	}
	claimedGross, err := v.rewardSources.FetchSuccessfulRewardAttemptEarnedTotal(ctx, row.ReceiverUserID.Int64)
	if err != nil {
		return nil, err
	}
	availableStructuredRewards := sourceTotal - claimedGross
	if availableStructuredRewards < 0 {
		availableStructuredRewards = 0
	}
	if row.EarnedRewards > availableStructuredRewards {
		return nil, NewError("reward amount exceeds structured reward balance")
	}
	row.SourceOrderID = sourceOrderID

	// Decode invoice and validate
	decoded, err := v.lnd.DecodePayReq(ctx, req.PaymentRequest)
	if err != nil {
		return nil, err
	}
	if decoded.PaymentHash != req.PaymentHash {
		return nil, NewError("decoded invoice hash mismatch")
	}
	if decoded.NumSatoshis != req.AmountSats {
		return nil, NewError("decoded invoice amount mismatch")
	}

	// Check invoice not expired
	dbExpired := isPast(row.PayoutExpiresAt)
	decodedExpiry := time.Unix(decoded.Timestamp+decoded.Expiry, 0).UTC()
	decodedExpired := decodedExpiry.Before(time.Now().UTC())
	if dbExpired || decodedExpired {
		reason := "decoded invoice expired"
		if dbExpired {
			reason = "invoice expired in RoboSats DB"
		}
		return nil, NewError(reason)
	}

	return row, nil
}

func (v *Validator) ValidateRewardSource(ctx context.Context, req httpapi.RewardSourceRequest) error {
	if req.Source != "robosats" {
		return NewError("source must be robosats")
	}
	if req.Kind != "reward_source" {
		return NewError("kind must be reward_source")
	}
	if strings.TrimSpace(req.RequestID) == "" {
		return NewError("request_id is required")
	}
	if req.SourceOrderID <= 0 {
		return NewError("source_order_id is required")
	}
	if req.ReceiverRobotID <= 0 {
		return NewError("receiver_robot_id is required")
	}
	if req.ReceiverUserID <= 0 {
		return NewError("receiver_user_id is required")
	}
	req.ReceiverRobotName = strings.TrimSpace(req.ReceiverRobotName)
	if req.ReceiverRobotName == "" {
		return NewError("receiver_robot_name is required")
	}
	if !supportedRewardSourceReason(req.Reason) {
		return NewError("reward source reason is not supported")
	}
	if req.RewardSats <= 0 || req.RewardSats >= v.cfg.MaxRewardWithdrawalSats {
		return NewError("reward amount is outside policy")
	}
	slashedHash := strings.ToLower(strings.TrimSpace(req.SlashedBondPaymentHash))
	stakedHash := strings.ToLower(strings.TrimSpace(req.StakedBondPaymentHash))
	if err := validateHexHash("staked_bond_payment_hash", stakedHash); err != nil {
		return err
	}
	if req.StakedBondSats <= 0 {
		return NewError("staked_bond_sats must be positive")
	}
	if req.StakedBondStatus <= 0 {
		return NewError("staked_bond_status is invalid")
	}

	identity, err := v.repo.FetchRobotIdentity(ctx, req.ReceiverUserID)
	if err != nil {
		return err
	}
	if identity == nil {
		return NewError("reward receiver robot not found")
	}
	if identity.RobotID != req.ReceiverRobotID || identity.Username != req.ReceiverRobotName {
		return NewError("reward receiver identity mismatch")
	}

	orderBonds, err := v.repo.FetchOrderBonds(ctx, req.SourceOrderID)
	if err != nil {
		return err
	}
	if orderBonds == nil {
		return NewError("source order not found")
	}
	receiverIsMaker := orderBonds.MakerID.Valid && orderBonds.MakerID.Int64 == req.ReceiverUserID
	receiverIsTaker := orderBonds.TakerID.Valid && orderBonds.TakerID.Int64 == req.ReceiverUserID
	if !receiverIsMaker && !receiverIsTaker {
		return NewError("reward receiver is not an order participant")
	}
	if !rewardSourceStakedBondMatchesOrder(orderBonds, receiverIsMaker, stakedHash) {
		return NewError("reward source staked bond does not match source order")
	}

	stakedBond, err := v.repo.FetchBondByPaymentHash(ctx, stakedHash)
	if err != nil {
		return err
	}
	if stakedBond == nil {
		return NewError("staked bond not found")
	}
	if !validBondConcept(stakedBond.Concept) {
		return NewError("reward source bond concept is invalid")
	}
	if stakedBond.NumSatoshis != req.StakedBondSats || stakedBond.Status != req.StakedBondStatus {
		return NewError("staked bond snapshot mismatch")
	}
	if !stakedBond.SenderID.Valid || stakedBond.SenderID.Int64 != req.ReceiverUserID {
		return NewError("staked bond is not receiver bond")
	}
	// The receiver's staked bond is the non-slashed bond. RoboSats records it in
	// its returned/unlocked final state, not as a settled LND invoice.

	if req.Reason == "counterparty_cancelled" {
		if slashedHash != "" || req.SlashedBondSats != 0 || req.SlashedBondStatus != 0 {
			return NewError("counterparty cancellation reward must not include slashed bond")
		}
		if rewardSourcePeerBondExists(orderBonds, receiverIsMaker) {
			return NewError("counterparty cancellation reward requires missing peer bond")
		}
		if req.RewardSats > req.StakedBondSats/2 {
			return NewError("reward exceeds cancellation reward policy")
		}
		return nil
	}

	if err := validateHexHash("slashed_bond_payment_hash", slashedHash); err != nil {
		return err
	}
	if slashedHash == stakedHash {
		return NewError("slashed and staked bonds must be different")
	}
	if req.SlashedBondSats <= 0 {
		return NewError("slashed_bond_sats must be positive")
	}
	if req.SlashedBondStatus != 2 {
		return NewError("slashed bond is not settled")
	}
	if req.RewardSats > minInt64(req.SlashedBondSats, req.StakedBondSats)/2 {
		return NewError("reward exceeds slashed bond policy")
	}
	if receiverIsMaker {
		if !sameNullableString(orderBonds.TakerBondID, slashedHash) {
			return NewError("reward source bonds do not match source order")
		}
	} else if receiverIsTaker {
		if !sameNullableString(orderBonds.MakerBondID, slashedHash) {
			return NewError("reward source bonds do not match source order")
		}
	}

	slashedBond, err := v.repo.FetchBondByPaymentHash(ctx, slashedHash)
	if err != nil {
		return err
	}
	if slashedBond == nil {
		return NewError("slashed bond not found")
	}
	if !validBondConcept(slashedBond.Concept) || !validBondConcept(stakedBond.Concept) {
		return NewError("reward source bond concept is invalid")
	}
	if slashedBond.NumSatoshis != req.SlashedBondSats || slashedBond.Status != req.SlashedBondStatus {
		return NewError("slashed bond snapshot mismatch")
	}
	if !slashedBond.SenderID.Valid || slashedBond.SenderID.Int64 == req.ReceiverUserID {
		return NewError("slashed bond is not peer bond")
	}
	if err := v.verifyBondSettledViaLND(ctx, slashedHash); err != nil {
		return err
	}
	return nil
}

func rewardCoordinatorFeeLimit(amountSats int64, cfg config.Config) int64 {
	ppm := cfg.RewardRoutingFeeLimitPPM
	if ppm <= 0 {
		ppm = 1000
	}
	minFlat := cfg.RewardMinFlatFeeLimitSats
	if minFlat <= 0 {
		minFlat = 2
	}
	proportional := decimal.NewFromInt(amountSats).
		Mul(decimal.NewFromInt(ppm)).
		Div(decimal.NewFromInt(1_000_000)).
		IntPart()
	if proportional > minFlat {
		return proportional
	}
	return minFlat
}

func rewardWithdrawalCooldown(cfg config.Config) time.Duration {
	hours := cfg.RewardWithdrawalCooldownHours
	if hours <= 0 {
		hours = 48
	}
	return time.Duration(hours) * time.Hour
}

func validateHexHash(field string, value string) error {
	if len(value) != 64 {
		return NewError(field + " must be 32-byte hex")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return NewError(field + " must be hex")
	}
	return nil
}

func validBondConcept(value int64) bool {
	return value == 0 || value == 1
}

func supportedRewardSourceReason(value string) bool {
	return value == "slashed_bond" || value == "counterparty_cancelled"
}

func sameNullableString(value sql.NullString, want string) bool {
	return value.Valid && strings.EqualFold(value.String, want)
}

func rewardSourceStakedBondMatchesOrder(orderBonds *robosats.OrderBonds, receiverIsMaker bool, stakedHash string) bool {
	if receiverIsMaker {
		return sameNullableString(orderBonds.MakerBondID, stakedHash)
	}
	return sameNullableString(orderBonds.TakerBondID, stakedHash)
}

func rewardSourcePeerBondExists(orderBonds *robosats.OrderBonds, receiverIsMaker bool) bool {
	if receiverIsMaker {
		return orderBonds.TakerBondID.Valid && orderBonds.TakerBondID.String != ""
	}
	return orderBonds.MakerBondID.Valid && orderBonds.MakerBondID.String != ""
}

func minInt64(a int64, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func (v *Validator) verifyEscrowSettledViaLND(ctx context.Context, escrowPaymentHash string) error {
	if escrowPaymentHash == "" {
		return NewError("escrow payment hash is empty")
	}
	invoice, err := v.lnd.LookupInvoice(ctx, escrowPaymentHash)
	if err != nil {
		return NewError("failed to lookup escrow invoice in LND: " + err.Error())
	}
	if !invoice.Found {
		return NewError("escrow hold invoice not found in LND")
	}
	if invoice.State != "SETTLED" {
		return NewError("escrow hold invoice is not SETTLED in LND (state=" + invoice.State + ")")
	}
	return nil
}

func (v *Validator) verifyBondSettledViaLND(ctx context.Context, bondPaymentHash string) error {
	if bondPaymentHash == "" {
		return nil
	}
	invoice, err := v.lnd.LookupInvoice(ctx, bondPaymentHash)
	if err != nil {
		return NewError("failed to lookup bond invoice in LND: " + err.Error())
	}
	if !invoice.Found {
		return NewError("bond hold invoice not found in LND")
	}
	if invoice.State != "SETTLED" {
		return NewError("bond hold invoice is not SETTLED in LND (state=" + invoice.State + ")")
	}
	return nil
}

func (v *Validator) verifyBondsNotSettledViaLND(ctx context.Context, makerBondID, takerBondID sql.NullString) error {
	if makerBondID.Valid && makerBondID.String != "" {
		invoice, err := v.lnd.LookupInvoice(ctx, makerBondID.String)
		if err != nil {
			return NewError("failed to lookup maker bond invoice in LND: " + err.Error())
		}
		if invoice.Found && invoice.State == "SETTLED" {
			return NewError("maker bond is SETTLED in LND, indicating a dispute claim")
		}
	}
	if takerBondID.Valid && takerBondID.String != "" {
		invoice, err := v.lnd.LookupInvoice(ctx, takerBondID.String)
		if err != nil {
			return NewError("failed to lookup taker bond invoice in LND: " + err.Error())
		}
		if invoice.Found && invoice.State == "SETTLED" {
			return NewError("taker bond is SETTLED in LND, indicating a dispute claim")
		}
	}
	return nil
}
