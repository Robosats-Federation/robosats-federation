package httpapi

type LNPayoutRequest struct {
	RequestID         string `json:"request_id"`
	Source            string `json:"source"`
	Kind              string `json:"kind"`
	OrderID           int64  `json:"order_id"`
	PaymentHash       string `json:"payment_hash"`
	PaymentRequest    string `json:"payment_request"`
	AmountSats        int64  `json:"amount_sats"`
	FeeLimitSat       int64  `json:"fee_limit_sat"`
	TimeoutSeconds    int64  `json:"timeout_seconds"`
	RoutingBudgetPPM  int64  `json:"routing_budget_ppm"`
	RoutingBudgetSats string `json:"routing_budget_sats"`
	LNPaymentStatus   int64  `json:"lnpayment_status"`
	OrderStatus       int64  `json:"order_status"`
	ReceiverUserID    *int64 `json:"receiver_user_id"`
	MakerUserID       *int64 `json:"maker_user_id"`
	TakerUserID       *int64 `json:"taker_user_id"`
	OrderType         int64  `json:"order_type"`
	AttemptNumber     int64  `json:"attempt_number"`
}

type OnchainPayoutRequest struct {
	RequestID            string `json:"request_id"`
	Source               string `json:"source"`
	Kind                 string `json:"kind"`
	OrderID              int64  `json:"order_id"`
	OnchainPaymentID     int64  `json:"onchain_payment_id"`
	Address              string `json:"address"`
	AmountSats           int64  `json:"amount_sats"`
	FeeRateSatVByte      int64  `json:"fee_rate_sat_vbyte"`
	Label                string `json:"label"`
	SpendUnconfirmed     bool   `json:"spend_unconfirmed"`
	GrossPayoutSats      *int64 `json:"gross_payout_sats"`
	MiningFeeSats        *int64 `json:"mining_fee_sats"`
	SwapFeeRate          string `json:"swap_fee_rate"`
	ReceiverUserID       *int64 `json:"receiver_user_id"`
	ReceiverRobotID      *int64 `json:"receiver_robot_id"`
	Concept              string `json:"concept"`
	OnchainPaymentStatus int64  `json:"onchain_payment_status"`
	OrderStatus          int64  `json:"order_status"`
	IsSwap               bool   `json:"is_swap"`
}

type KeysendRequest struct {
	RequestID      string `json:"request_id"`
	Source         string `json:"source"`
	Kind           string `json:"kind"`
	TargetPubkey   string `json:"target_pubkey"`
	Message        string `json:"message"`
	AmountSats     int64  `json:"amount_sats"`
	FeeLimitSat    int64  `json:"fee_limit_sat"`
	TimeoutSeconds int64  `json:"timeout_seconds"`
	Sign           bool   `json:"sign"`
	OrderID        int64  `json:"order_id,omitempty"`
}

type PaymentResponse struct {
	Decision          string  `json:"decision"`
	Status            string  `json:"status"`
	PaymentHash       string  `json:"payment_hash,omitempty"`
	FeeMsat           int64   `json:"fee_msat,omitempty"`
	FeeSat            float64 `json:"fee_sat,omitempty"`
	PaymentPreimage   string  `json:"payment_preimage,omitempty"`
	FailureReason     *string `json:"failure_reason"`
	FailureReasonCode *int64  `json:"failure_reason_code,omitempty"`
	Message           string  `json:"message"`
	RawLNDStatus      string  `json:"raw_lnd_status,omitempty"`
}

type OnchainResponse struct {
	Decision         string `json:"decision"`
	Status           string `json:"status"`
	OrderID          int64  `json:"order_id"`
	OnchainPaymentID int64  `json:"onchain_payment_id"`
	TxID             string `json:"txid"`
	Message          string `json:"message"`
}
