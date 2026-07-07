package httpapi

type VersionResponse struct {
	Version       string   `json:"version"`
	Commit        string   `json:"commit,omitempty"`
	CommitHash    string   `json:"commit_hash,omitempty"`
	AppMajor      uint32   `json:"app_major,omitempty"`
	AppMinor      uint32   `json:"app_minor,omitempty"`
	AppPatch      uint32   `json:"app_patch,omitempty"`
	AppPreRelease string   `json:"app_pre_release,omitempty"`
	BuildTags     []string `json:"build_tags,omitempty"`
	GoVersion     string   `json:"go_version,omitempty"`
}

type DecodePayReqRequest struct {
	PaymentRequest string `json:"payment_request"`
}

type DecodePayReqResponse struct {
	Destination     string      `json:"destination,omitempty"`
	PaymentHash     string      `json:"payment_hash"`
	NumSatoshis     int64       `json:"num_satoshis"`
	Timestamp       int64       `json:"timestamp"`
	Expiry          int64       `json:"expiry"`
	Description     string      `json:"description,omitempty"`
	DescriptionHash string      `json:"description_hash,omitempty"`
	FallbackAddr    string      `json:"fallback_addr,omitempty"`
	CltvExpiry      int64       `json:"cltv_expiry,omitempty"`
	RouteHints      []RouteHint `json:"route_hints"`
	PaymentAddr     string      `json:"payment_addr,omitempty"`
	NumMsat         int64       `json:"num_msat,omitempty"`
}

type RouteHint struct {
	HopHints []HopHint `json:"hop_hints"`
}

type HopHint struct {
	NodeID                    string `json:"node_id"`
	ChanID                    uint64 `json:"chan_id"`
	FeeBaseMsat               uint32 `json:"fee_base_msat"`
	FeeProportionalMillionths uint32 `json:"fee_proportional_millionths"`
	CltvExpiryDelta           uint32 `json:"cltv_expiry_delta"`
}

type EstimateFeeRequest struct {
	AmountSats int64 `json:"amount_sats"`
	TargetConf int32 `json:"target_conf"`
	MinConfs   int32 `json:"min_confs"`
}

type EstimateFeeResponse struct {
	FeeSat       int64  `json:"fee_sat"`
	SatPerVbyte  uint64 `json:"sat_per_vbyte"`
	DummyAddress string `json:"dummy_address,omitempty"`
}

type WalletBalanceResponse struct {
	TotalBalance              int64 `json:"total_balance"`
	ConfirmedBalance          int64 `json:"confirmed_balance"`
	UnconfirmedBalance        int64 `json:"unconfirmed_balance"`
	LockedBalance             int64 `json:"locked_balance"`
	ReservedBalanceAnchorChan int64 `json:"reserved_balance_anchor_chan"`
}

type ChannelBalanceResponse struct {
	Balance                   int64 `json:"balance"`
	PendingOpenBalance        int64 `json:"pending_open_balance"`
	LocalBalanceSat           int64 `json:"local_balance_sat"`
	RemoteBalanceSat          int64 `json:"remote_balance_sat"`
	UnsettledLocalBalanceSat  int64 `json:"unsettled_local_balance_sat"`
	UnsettledRemoteBalanceSat int64 `json:"unsettled_remote_balance_sat"`
}

type AddHoldInvoiceRequest struct {
	Memo          string `json:"memo"`
	AmountSats    int64  `json:"amount_sats"`
	InvoiceExpiry int64  `json:"invoice_expiry"`
	CltvExpiry    uint64 `json:"cltv_expiry"`
}

type HoldInvoiceResponse struct {
	PaymentRequest string `json:"payment_request"`
	PaymentHash    string `json:"payment_hash"`
	Preimage       string `json:"preimage"`
	CreatedAt      string `json:"created_at"`
	ExpiresAt      string `json:"expires_at"`
	CltvExpiry     uint64 `json:"cltv_expiry"`
	AddIndex       uint64 `json:"add_index,omitempty"`
	PaymentAddr    string `json:"payment_addr,omitempty"`
}

type PaymentHashRequest struct {
	PaymentHash string `json:"payment_hash"`
}

type PreimageRequest struct {
	Preimage string `json:"preimage"`
}

type OperationResponse struct {
	Ok      bool   `json:"ok"`
	Message string `json:"message"`
}

type LookupInvoiceResponse struct {
	Found        bool                `json:"found"`
	State        string              `json:"state"`
	StateCode    int32               `json:"state_code"`
	PaymentHash  string              `json:"payment_hash,omitempty"`
	PaymentReq   string              `json:"payment_request,omitempty"`
	ValueSat     int64               `json:"value_sat,omitempty"`
	AmtPaidSat   int64               `json:"amt_paid_sat,omitempty"`
	AmtPaidMsat  int64               `json:"amt_paid_msat,omitempty"`
	CreationDate int64               `json:"creation_date,omitempty"`
	SettleDate   int64               `json:"settle_date,omitempty"`
	Expiry       int64               `json:"expiry,omitempty"`
	ExpiryHeight int32               `json:"expiry_height,omitempty"`
	HTLCs        []LookupInvoiceHTLC `json:"htlcs"`
	Message      string              `json:"message,omitempty"`
}

type LookupInvoiceHTLC struct {
	ChanID       uint64 `json:"chan_id"`
	HTLCIndex    uint64 `json:"htlc_index"`
	AmtMsat      uint64 `json:"amt_msat"`
	AcceptHeight int32  `json:"accept_height"`
	ExpiryHeight int32  `json:"expiry_height"`
	State        string `json:"state"`
	StateCode    int32  `json:"state_code"`
}

type RewardWithdrawalRequest struct {
	RequestID         string `json:"request_id"`
	Source            string `json:"source"`
	Kind              string `json:"kind"`
	PaymentHash       string `json:"payment_hash"`
	PaymentRequest    string `json:"payment_request"`
	AmountSats        int64  `json:"amount_sats"`
	FeeLimitSat       int64  `json:"fee_limit_sat"`
	TimeoutSeconds    int64  `json:"timeout_seconds"`
	RoutingBudgetPPM  int64  `json:"routing_budget_ppm"`
	RoutingBudgetSats string `json:"routing_budget_sats"`
	ReceiverUserID    *int64 `json:"receiver_user_id"`
	ReceiverRobotName string `json:"receiver_robot_name"`
}

type RewardSourceRequest struct {
	RequestID              string `json:"request_id"`
	Source                 string `json:"source"`
	Kind                   string `json:"kind"`
	SourceOrderID          int64  `json:"source_order_id"`
	ReceiverRobotID        int64  `json:"receiver_robot_id"`
	ReceiverUserID         int64  `json:"receiver_user_id"`
	ReceiverRobotName      string `json:"receiver_robot_name"`
	RewardSats             int64  `json:"reward_sats"`
	SlashedBondPaymentHash string `json:"slashed_bond_payment_hash"`
	SlashedBondSats        int64  `json:"slashed_bond_sats"`
	SlashedBondStatus      int64  `json:"slashed_bond_status"`
	StakedBondPaymentHash  string `json:"staked_bond_payment_hash"`
	StakedBondSats         int64  `json:"staked_bond_sats"`
	StakedBondStatus       int64  `json:"staked_bond_status"`
	Reason                 string `json:"reason"`
}
