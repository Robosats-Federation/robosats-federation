package lnd

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/invoicesrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/lightningnetwork/lnd/lnrpc/verrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	"payout_policy_middleware/internal/config"
	"payout_policy_middleware/internal/httpapi"
)

type Client struct {
	cfg       config.Config
	conn      *grpc.ClientConn
	lightning lnrpc.LightningClient
	router    routerrpc.RouterClient
	invoices  invoicesrpc.InvoicesClient
	versioner verrpc.VersionerClient
}

type macaroonCredential struct {
	hex string
}

func (m macaroonCredential) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{"macaroon": m.hex}, nil
}

func (m macaroonCredential) RequireTransportSecurity() bool {
	return true
}

func New(ctx context.Context, cfg config.Config) (*Client, error) {
	cert, err := config.ReadSecret(cfg.LNDTLSCertPath, cfg.LNDTLSCertBase64, "LND TLS cert")
	if err != nil {
		return nil, err
	}
	macaroon, err := config.ReadSecret(cfg.LNDMacaroonPath, cfg.LNDMacaroonBase64, "LND macaroon")
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(cert) {
		return nil, fmt.Errorf("failed to parse LND TLS cert")
	}
	creds := credentials.NewClientTLSFromCert(pool, "")
	conn, err := grpc.DialContext(
		ctx,
		cfg.LNDGRPCHost,
		grpc.WithTransportCredentials(creds),
		grpc.WithPerRPCCredentials(macaroonCredential{hex: hex.EncodeToString(macaroon)}),
	)
	if err != nil {
		return nil, err
	}
	return &Client{
		cfg:       cfg,
		conn:      conn,
		lightning: lnrpc.NewLightningClient(conn),
		router:    routerrpc.NewRouterClient(conn),
		invoices:  invoicesrpc.NewInvoicesClient(conn),
		versioner: verrpc.NewVersionerClient(conn),
	}, nil
}

func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) DecodePayReq(ctx context.Context, paymentRequest string) (*DecodedPayReq, error) {
	decoded, err := c.DecodePayReqFull(ctx, paymentRequest)
	if err != nil {
		return nil, err
	}
	return &DecodedPayReq{
		PaymentHash: decoded.PaymentHash,
		NumSatoshis: decoded.NumSatoshis,
		Timestamp:   decoded.Timestamp,
		Expiry:      decoded.Expiry,
	}, nil
}

func (c *Client) TrackPayment(ctx context.Context, paymentHash string) (*httpapi.PaymentResponse, error) {
	hash, err := hex.DecodeString(paymentHash)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(c.cfg.LNTrackTimeoutSeconds)*time.Second)
	defer cancel()
	stream, err := c.router.TrackPaymentV2(ctx, &routerrpc.TrackPaymentRequest{PaymentHash: hash})
	if err != nil {
		return nil, err
	}
	payment, err := stream.Recv()
	if err != nil {
		return nil, err
	}
	alreadyPaid := paymentStatusName(payment.Status) == "SUCCEEDED"
	return normalizePayment(payment, alreadyPaid), nil
}

func (c *Client) TryTrackPayment(ctx context.Context, paymentHash string) (*httpapi.PaymentResponse, error) {
	result, err := c.TrackPayment(ctx, paymentHash)
	if err == nil {
		return result, nil
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "payment isn't initiated") || strings.Contains(msg, "not found") {
		return nil, nil
	}
	if status.Code(err) == codes.DeadlineExceeded {
		return nil, nil
	}
	return nil, err
}

func (c *Client) SendPayment(ctx context.Context, req httpapi.LNPayoutRequest) (*httpapi.PaymentResponse, error) {
	request := &routerrpc.SendPaymentRequest{
		PaymentRequest:   req.PaymentRequest,
		FeeLimitSat:      req.FeeLimitSat,
		TimeoutSeconds:   int32(req.TimeoutSeconds),
		AllowSelfPayment: true,
		Amp:              true,
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(c.cfg.LNStreamTimeoutSeconds)*time.Second)
	defer cancel()
	stream, err := c.router.SendPaymentV2(ctx, request)
	if err != nil {
		return nil, err
	}

	var latest *lnrpc.Payment
	for {
		payment, err := stream.Recv()
		if err != nil {
			if response := c.handleSendPaymentError(ctx, req.PaymentHash, err, latest); response != nil {
				return response, nil
			}
			return nil, err
		}
		latest = payment
		statusName := paymentStatusName(payment.Status)
		if statusName == "SUCCEEDED" || statusName == "FAILED" {
			break
		}
	}
	if latest != nil {
		return normalizePayment(latest, false), nil
	}
	return &httpapi.PaymentResponse{
		Decision:    "ERROR",
		Status:      "ERROR",
		PaymentHash: req.PaymentHash,
		Message:     "LND returned no payment updates",
	}, nil
}

func (c *Client) handleSendPaymentError(ctx context.Context, paymentHash string, err error, latest *lnrpc.Payment) *httpapi.PaymentResponse {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "invoice expired") {
		code := int64(4)
		return &httpapi.PaymentResponse{
			Decision:          "APPROVED",
			Status:            "EXPIRED",
			PaymentHash:       paymentHash,
			FailureReasonCode: &code,
			Message:           "invoice expired",
		}
	}
	if strings.Contains(msg, "payment is in transition") || strings.Contains(msg, "invoice is already paid") {
		tracked, trackErr := c.TryTrackPayment(ctx, paymentHash)
		if trackErr == nil && tracked != nil {
			return tracked
		}
	}
	if status.Code(err) == codes.DeadlineExceeded && latest != nil {
		return normalizePayment(latest, false)
	}
	return nil
}

func (c *Client) SendCoins(ctx context.Context, req httpapi.OnchainPayoutRequest) (*httpapi.OnchainResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	resp, err := c.lightning.SendCoins(ctx, &lnrpc.SendCoinsRequest{
		Addr:             req.Address,
		Amount:           req.AmountSats,
		SatPerVbyte:      uint64(req.FeeRateSatVByte),
		Label:            req.Label,
		SpendUnconfirmed: req.SpendUnconfirmed,
	})
	if err != nil {
		return nil, err
	}
	return &httpapi.OnchainResponse{
		Decision:         "APPROVED",
		Status:           "BROADCASTED",
		OrderID:          req.OrderID,
		OnchainPaymentID: req.OnchainPaymentID,
		TxID:             resp.Txid,
		Message:          "ok",
	}, nil
}

func (c *Client) GenerateKeysendSecret() (string, string, error) {
	preimage := make([]byte, 32)
	if _, err := rand.Read(preimage); err != nil {
		return "", "", err
	}
	hash := sha256.Sum256(preimage)
	return hex.EncodeToString(preimage), hex.EncodeToString(hash[:]), nil
}

func (c *Client) SendKeysend(ctx context.Context, req httpapi.KeysendRequest, preimage string, paymentHash string) (*httpapi.PaymentResponse, error) {
	preimageBytes, err := hex.DecodeString(preimage)
	if err != nil {
		return nil, err
	}
	paymentHashBytes, err := hex.DecodeString(paymentHash)
	if err != nil {
		return nil, err
	}
	dest, err := hex.DecodeString(req.TargetPubkey)
	if err != nil {
		return nil, err
	}
	records := map[uint64][]byte{
		5482373484: preimageBytes,
	}
	if req.Message != "" {
		records[34349334] = []byte(req.Message)
	}
	request := &routerrpc.SendPaymentRequest{
		Dest:              dest,
		DestCustomRecords: records,
		FeeLimitSat:       req.FeeLimitSat,
		TimeoutSeconds:    int32(req.TimeoutSeconds),
		Amt:               req.AmountSats,
		PaymentHash:       paymentHashBytes,
		AllowSelfPayment:  false,
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutSeconds+5)*time.Second)
	defer cancel()
	stream, err := c.router.SendPaymentV2(ctx, request)
	if err != nil {
		return nil, err
	}
	var latest *lnrpc.Payment
	for {
		payment, err := stream.Recv()
		if err != nil {
			if status.Code(err) == codes.DeadlineExceeded && latest != nil {
				return normalizeKeysendPayment(latest, paymentHash, preimage), nil
			}
			return nil, err
		}
		latest = payment
		statusName := paymentStatusName(payment.Status)
		if statusName == "SUCCEEDED" || statusName == "FAILED" {
			break
		}
	}
	if latest != nil {
		return normalizeKeysendPayment(latest, paymentHash, preimage), nil
	}
	return &httpapi.PaymentResponse{
		Decision:        "ERROR",
		Status:          "ERROR",
		PaymentHash:     paymentHash,
		PaymentPreimage: preimage,
		Message:         "LND returned no keysend updates",
	}, nil
}

func paymentStatusName(status lnrpc.Payment_PaymentStatus) string {
	switch status {
	case lnrpc.Payment_IN_FLIGHT:
		return "IN_FLIGHT"
	case lnrpc.Payment_SUCCEEDED:
		return "SUCCEEDED"
	case lnrpc.Payment_FAILED:
		return "FAILED"
	case lnrpc.Payment_INITIATED:
		return "INITIATED"
	case lnrpc.Payment_UNKNOWN:
		return "UNKNOWN"
	default:
		return "ERROR"
	}
}

func normalizePayment(payment *lnrpc.Payment, alreadyPaid bool) *httpapi.PaymentResponse {
	statusName := paymentStatusName(payment.Status)
	if alreadyPaid && statusName == "SUCCEEDED" {
		statusName = "ALREADY_PAID"
	}
	feeMsat := payment.FeeMsat
	failureCode := int64(payment.FailureReason)
	return &httpapi.PaymentResponse{
		Decision:          "APPROVED",
		Status:            statusName,
		PaymentHash:       payment.PaymentHash,
		FeeMsat:           feeMsat,
		FeeSat:            float64(feeMsat) / 1000,
		PaymentPreimage:   payment.PaymentPreimage,
		FailureReason:     nil,
		FailureReasonCode: &failureCode,
		Message:           "ok",
		RawLNDStatus:      statusName,
	}
}

func normalizeKeysendPayment(payment *lnrpc.Payment, paymentHash string, preimage string) *httpapi.PaymentResponse {
	result := normalizePayment(payment, false)
	result.PaymentHash = paymentHash
	result.PaymentPreimage = preimage
	return result
}
