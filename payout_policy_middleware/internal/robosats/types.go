package robosats

import "database/sql"

type LNRow struct {
	OrderID           int64
	OrderStatus       int64
	OrderType         int64
	IsSwap            bool
	PayoutID          string
	TradeEscrowID     string
	MakerBondID       sql.NullString
	TakerBondID       sql.NullString
	MakerID           sql.NullInt64
	TakerID           sql.NullInt64
	LastSatoshis      sql.NullInt64
	Proceeds          sql.NullInt64
	PayoutHash        string
	PayoutStatus      int64
	PayoutType        int64
	PayoutConcept     int64
	PayoutSats        int64
	RoutingBudgetPPM  int64
	RoutingBudgetSats string
	PayoutReceiverID  sql.NullInt64
	PayoutSenderID    sql.NullInt64
	PayoutInvoice     string
	PayoutExpiresAt   sql.NullTime
	EscrowStatus      int64
	EscrowSats        int64
	MakerBondStatus   sql.NullInt64
	TakerBondStatus   sql.NullInt64

	PolicyInvoiceExpired       bool
	PolicyInvoiceExpiredReason string
}

type OnchainRow struct {
	OrderID          int64
	OrderStatus      int64
	OrderType        int64
	IsSwap           bool
	PayoutTxID       int64
	TradeEscrowID    string
	MakerID          sql.NullInt64
	TakerID          sql.NullInt64
	OnchainPaymentID int64
	OnchainConcept   int64
	OnchainStatus    int64
	Broadcasted      bool
	Address          string
	TxID             sql.NullString
	NumSatoshis      int64
	SentSatoshis     int64
	MiningFeeRate    string
	MiningFeeSats    int64
	SwapFeeRate      string
	PayoutReceiverID sql.NullInt64
	EscrowStatus     int64
	EscrowSats       int64
}

type DevfundOrder struct {
	OrderID     int64
	OrderStatus int64
	Proceeds    sql.NullInt64
}

type BondInfo struct {
	PaymentHash string
	NumSatoshis int64
	Status      int64
	Concept     int64
	SenderID    sql.NullInt64
}

type LNPaymentLogContext struct {
	PaymentHash      string
	Type             int64
	Concept          int64
	Status           int64
	NumSatoshis      int64
	SenderUserID     sql.NullInt64
	SenderRobotID    sql.NullInt64
	SenderUsername   sql.NullString
	ReceiverUserID   sql.NullInt64
	ReceiverRobotID  sql.NullInt64
	ReceiverUsername sql.NullString
	OrderID          sql.NullInt64
	OrderStatus      sql.NullInt64
	OrderType        sql.NullInt64
}

type RobotIdentity struct {
	UserID   int64
	RobotID  int64
	Username string
}

type OrderBonds struct {
	OrderID     int64
	MakerID     sql.NullInt64
	TakerID     sql.NullInt64
	MakerBondID sql.NullString
	TakerBondID sql.NullString
}

type RewardSource struct {
	SourceOrderID          int64
	ReceiverRobotID        int64
	ReceiverUserID         int64
	ReceiverRobotName      string
	RewardSats             int64
	SlashedBondPaymentHash string
	SlashedBondSats        int64
	SlashedBondStatus      int64
	StakedBondPaymentHash  string
	StakedBondSats         int64
	StakedBondStatus       int64
	Reason                 string
}

type RewardRow struct {
	ReceiverUserID    sql.NullInt64
	ReceiverRobotID   int64
	ReceiverUsername  string
	EarnedRewards     int64
	PayoutHash        string
	PayoutInvoice     string
	PayoutSats        int64
	PayoutExpiresAt   sql.NullTime
	RoutingBudgetPPM  int64
	RoutingBudgetSats string
	SourceOrderID     int64
}
