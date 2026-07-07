package lnd

import (
	"context"

	"payout_policy_middleware/internal/httpapi"
)

type DecodedPayReq struct {
	PaymentHash string
	NumSatoshis int64
	Timestamp   int64
	Expiry      int64
}

type Interface interface {
	GetVersion(ctx context.Context) (*httpapi.VersionResponse, error)
	DecodePayReq(ctx context.Context, paymentRequest string) (*DecodedPayReq, error)
	DecodePayReqFull(ctx context.Context, paymentRequest string) (*httpapi.DecodePayReqResponse, error)
	EstimateFee(ctx context.Context, req httpapi.EstimateFeeRequest) (*httpapi.EstimateFeeResponse, error)
	WalletBalance(ctx context.Context) (*httpapi.WalletBalanceResponse, error)
	ChannelBalance(ctx context.Context) (*httpapi.ChannelBalanceResponse, error)
	AddHoldInvoice(ctx context.Context, req httpapi.AddHoldInvoiceRequest) (*httpapi.HoldInvoiceResponse, error)
	CancelInvoice(ctx context.Context, paymentHash string) error
	SettleInvoice(ctx context.Context, preimage string) error
	LookupInvoice(ctx context.Context, paymentHash string) (*httpapi.LookupInvoiceResponse, error)
	TryTrackPayment(ctx context.Context, paymentHash string) (*httpapi.PaymentResponse, error)
	SendPayment(ctx context.Context, req httpapi.LNPayoutRequest) (*httpapi.PaymentResponse, error)
	SendCoins(ctx context.Context, req httpapi.OnchainPayoutRequest) (*httpapi.OnchainResponse, error)
	GenerateKeysendSecret() (preimage string, paymentHash string, err error)
	SendKeysend(ctx context.Context, req httpapi.KeysendRequest, preimage string, paymentHash string) (*httpapi.PaymentResponse, error)
}
