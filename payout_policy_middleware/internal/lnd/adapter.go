package lnd

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/invoicesrpc"
	"github.com/lightningnetwork/lnd/lnrpc/verrpc"

	"payout_policy_middleware/internal/httpapi"
)

func (c *Client) GetVersion(ctx context.Context) (*httpapi.VersionResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.adapterRPCTimeout())
	defer cancel()
	version, err := c.versioner.GetVersion(ctx, &verrpc.VersionRequest{})
	if err != nil {
		return nil, err
	}
	return &httpapi.VersionResponse{
		Version:       normalizedVersion(version.GetVersion()),
		Commit:        version.GetCommit(),
		CommitHash:    version.GetCommitHash(),
		AppMajor:      version.GetAppMajor(),
		AppMinor:      version.GetAppMinor(),
		AppPatch:      version.GetAppPatch(),
		AppPreRelease: version.GetAppPreRelease(),
		BuildTags:     version.GetBuildTags(),
		GoVersion:     version.GetGoVersion(),
	}, nil
}

func (c *Client) DecodePayReqFull(ctx context.Context, paymentRequest string) (*httpapi.DecodePayReqResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.adapterRPCTimeout())
	defer cancel()
	decoded, err := c.lightning.DecodePayReq(ctx, &lnrpc.PayReqString{PayReq: paymentRequest})
	if err != nil {
		return nil, err
	}
	return decodePayReqResponse(decoded), nil
}

func (c *Client) EstimateFee(ctx context.Context, req httpapi.EstimateFeeRequest) (*httpapi.EstimateFeeResponse, error) {
	if req.AmountSats <= 0 {
		return nil, errors.New("amount_sats must be greater than zero")
	}
	if req.TargetConf <= 0 {
		return nil, errors.New("target_conf must be greater than zero")
	}
	ctx, cancel := context.WithTimeout(ctx, c.adapterSlowTimeout())
	defer cancel()
	info, err := c.lightning.GetInfo(ctx, &lnrpc.GetInfoRequest{})
	if err != nil {
		return nil, err
	}
	addr := dummyFeeAddress(info)
	fee, err := c.lightning.EstimateFee(ctx, &lnrpc.EstimateFeeRequest{
		AddrToAmount:     map[string]int64{addr: req.AmountSats},
		TargetConf:       req.TargetConf,
		MinConfs:         req.MinConfs,
		SpendUnconfirmed: false,
	})
	if err != nil {
		return nil, err
	}
	return &httpapi.EstimateFeeResponse{
		FeeSat:       fee.GetFeeSat(),
		SatPerVbyte:  fee.GetSatPerVbyte(),
		DummyAddress: addr,
	}, nil
}

func (c *Client) WalletBalance(ctx context.Context) (*httpapi.WalletBalanceResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.adapterRPCTimeout())
	defer cancel()
	balance, err := c.lightning.WalletBalance(ctx, &lnrpc.WalletBalanceRequest{})
	if err != nil {
		return nil, err
	}
	return &httpapi.WalletBalanceResponse{
		TotalBalance:              balance.GetTotalBalance(),
		ConfirmedBalance:          balance.GetConfirmedBalance(),
		UnconfirmedBalance:        balance.GetUnconfirmedBalance(),
		LockedBalance:             balance.GetLockedBalance(),
		ReservedBalanceAnchorChan: balance.GetReservedBalanceAnchorChan(),
	}, nil
}

func (c *Client) ChannelBalance(ctx context.Context) (*httpapi.ChannelBalanceResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.adapterRPCTimeout())
	defer cancel()
	balance, err := c.lightning.ChannelBalance(ctx, &lnrpc.ChannelBalanceRequest{})
	if err != nil {
		return nil, err
	}
	return &httpapi.ChannelBalanceResponse{
		Balance:                   balance.GetBalance(),
		PendingOpenBalance:        balance.GetPendingOpenBalance(),
		LocalBalanceSat:           amountSat(balance.GetLocalBalance()),
		RemoteBalanceSat:          amountSat(balance.GetRemoteBalance()),
		UnsettledLocalBalanceSat:  amountSat(balance.GetUnsettledLocalBalance()),
		UnsettledRemoteBalanceSat: amountSat(balance.GetUnsettledRemoteBalance()),
	}, nil
}

func (c *Client) AddHoldInvoice(ctx context.Context, req httpapi.AddHoldInvoiceRequest) (*httpapi.HoldInvoiceResponse, error) {
	if req.AmountSats <= 0 {
		return nil, errors.New("amount_sats must be greater than zero")
	}
	if req.InvoiceExpiry <= 0 {
		return nil, errors.New("invoice_expiry must be greater than zero")
	}
	preimage := make([]byte, 32)
	if _, err := rand.Read(preimage); err != nil {
		return nil, err
	}
	hash := sha256.Sum256(preimage)
	expiry := req.InvoiceExpiry * 3 / 2
	if expiry <= req.InvoiceExpiry {
		expiry = req.InvoiceExpiry + 1
	}
	ctx, cancel := context.WithTimeout(ctx, c.adapterSlowTimeout())
	defer cancel()
	invoice, err := c.invoices.AddHoldInvoice(ctx, &invoicesrpc.AddHoldInvoiceRequest{
		Memo:       req.Memo,
		Hash:       hash[:],
		Value:      req.AmountSats,
		Expiry:     expiry,
		CltvExpiry: req.CltvExpiry,
	})
	if err != nil {
		return nil, err
	}
	decoded, err := c.DecodePayReqFull(ctx, invoice.GetPaymentRequest())
	if err != nil {
		return nil, err
	}
	created := time.Unix(decoded.Timestamp, 0).UTC()
	expires := created.Add(time.Duration(decoded.Expiry) * time.Second)
	return &httpapi.HoldInvoiceResponse{
		PaymentRequest: invoice.GetPaymentRequest(),
		PaymentHash:    decoded.PaymentHash,
		Preimage:       hex.EncodeToString(preimage),
		CreatedAt:      created.Format(time.RFC3339),
		ExpiresAt:      expires.Format(time.RFC3339),
		CltvExpiry:     req.CltvExpiry,
		AddIndex:       invoice.GetAddIndex(),
		PaymentAddr:    hex.EncodeToString(invoice.GetPaymentAddr()),
	}, nil
}

func (c *Client) CancelInvoice(ctx context.Context, paymentHash string) error {
	hash, err := decode32Hex(paymentHash, "payment_hash")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, c.adapterRPCTimeout())
	defer cancel()
	_, err = c.invoices.CancelInvoice(ctx, &invoicesrpc.CancelInvoiceMsg{PaymentHash: hash})
	return err
}

func (c *Client) SettleInvoice(ctx context.Context, preimage string) error {
	preimageBytes, err := decode32Hex(preimage, "preimage")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, c.adapterRPCTimeout())
	defer cancel()
	_, err = c.invoices.SettleInvoice(ctx, &invoicesrpc.SettleInvoiceMsg{Preimage: preimageBytes})
	return err
}

func (c *Client) LookupInvoice(ctx context.Context, paymentHash string) (*httpapi.LookupInvoiceResponse, error) {
	hash, err := decode32Hex(paymentHash, "payment_hash")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.adapterRPCTimeout())
	defer cancel()
	invoice, err := c.invoices.LookupInvoiceV2(ctx, &invoicesrpc.LookupInvoiceMsg{
		InvoiceRef: &invoicesrpc.LookupInvoiceMsg_PaymentHash{PaymentHash: hash},
	})
	if err != nil {
		if isInvoiceNotFound(err) {
			return &httpapi.LookupInvoiceResponse{
				Found:       false,
				State:       "NOT_FOUND",
				StateCode:   -1,
				PaymentHash: paymentHash,
				Message:     err.Error(),
				HTLCs:       []httpapi.LookupInvoiceHTLC{},
			}, nil
		}
		return nil, err
	}
	return lookupInvoiceResponse(paymentHash, invoice), nil
}

func (c *Client) adapterRPCTimeout() time.Duration {
	seconds := c.cfg.LNDAdapterRPCTimeoutSeconds
	if seconds <= 0 {
		seconds = 5
	}
	return time.Duration(seconds) * time.Second
}

func (c *Client) adapterSlowTimeout() time.Duration {
	seconds := c.cfg.LNDAdapterSlowTimeoutSeconds
	if seconds <= 0 {
		seconds = 10
	}
	return time.Duration(seconds) * time.Second
}

func decodePayReqResponse(decoded *lnrpc.PayReq) *httpapi.DecodePayReqResponse {
	return &httpapi.DecodePayReqResponse{
		Destination:     decoded.GetDestination(),
		PaymentHash:     decoded.GetPaymentHash(),
		NumSatoshis:     decoded.GetNumSatoshis(),
		Timestamp:       decoded.GetTimestamp(),
		Expiry:          decoded.GetExpiry(),
		Description:     decoded.GetDescription(),
		DescriptionHash: decoded.GetDescriptionHash(),
		FallbackAddr:    decoded.GetFallbackAddr(),
		CltvExpiry:      decoded.GetCltvExpiry(),
		RouteHints:      routeHints(decoded.GetRouteHints()),
		PaymentAddr:     hex.EncodeToString(decoded.GetPaymentAddr()),
		NumMsat:         decoded.GetNumMsat(),
	}
}

func routeHints(hints []*lnrpc.RouteHint) []httpapi.RouteHint {
	out := make([]httpapi.RouteHint, 0, len(hints))
	for _, hint := range hints {
		hops := make([]httpapi.HopHint, 0, len(hint.GetHopHints()))
		for _, hop := range hint.GetHopHints() {
			hops = append(hops, httpapi.HopHint{
				NodeID:                    hop.GetNodeId(),
				ChanID:                    hop.GetChanId(),
				FeeBaseMsat:               hop.GetFeeBaseMsat(),
				FeeProportionalMillionths: hop.GetFeeProportionalMillionths(),
				CltvExpiryDelta:           hop.GetCltvExpiryDelta(),
			})
		}
		out = append(out, httpapi.RouteHint{HopHints: hops})
	}
	return out
}

func lookupInvoiceResponse(paymentHash string, invoice *lnrpc.Invoice) *httpapi.LookupInvoiceResponse {
	htlcs := make([]httpapi.LookupInvoiceHTLC, 0, len(invoice.GetHtlcs()))
	var expiryHeight int32
	for _, htlc := range invoice.GetHtlcs() {
		if htlc.GetExpiryHeight() > expiryHeight {
			expiryHeight = htlc.GetExpiryHeight()
		}
		htlcs = append(htlcs, httpapi.LookupInvoiceHTLC{
			ChanID:       htlc.GetChanId(),
			HTLCIndex:    htlc.GetHtlcIndex(),
			AmtMsat:      htlc.GetAmtMsat(),
			AcceptHeight: htlc.GetAcceptHeight(),
			ExpiryHeight: htlc.GetExpiryHeight(),
			State:        htlc.GetState().String(),
			StateCode:    int32(htlc.GetState()),
		})
	}
	return &httpapi.LookupInvoiceResponse{
		Found:        true,
		State:        invoice.GetState().String(),
		StateCode:    int32(invoice.GetState()),
		PaymentHash:  paymentHash,
		PaymentReq:   invoice.GetPaymentRequest(),
		ValueSat:     invoice.GetValue(),
		AmtPaidSat:   invoice.GetAmtPaidSat(),
		AmtPaidMsat:  invoice.GetAmtPaidMsat(),
		CreationDate: invoice.GetCreationDate(),
		SettleDate:   invoice.GetSettleDate(),
		Expiry:       invoice.GetExpiry(),
		ExpiryHeight: expiryHeight,
		HTLCs:        htlcs,
	}
}

func dummyFeeAddress(info *lnrpc.GetInfoResponse) string {
	if info.GetTestnet() {
		return "tb1qehyqhruxwl2p5pt52k6nxj4v8wwc3f3pg7377x"
	}
	for _, chain := range info.GetChains() {
		if chain.GetNetwork() == "regtest" {
			return "bcrt1q3w8xja7knmycsglnxg2xzjq8uv9u7jdwau25nl"
		}
	}
	return "bc1qgxwaqe4m9mypd7ltww53yv3lyxhcfnhzzvy5j3"
}

func amountSat(amount *lnrpc.Amount) int64 {
	if amount == nil {
		return 0
	}
	return int64(amount.GetSat())
}

func normalizedVersion(version string) string {
	if version == "" || strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}

func decode32Hex(value string, name string) ([]byte, error) {
	decoded, err := hex.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("%s must be hex: %w", name, err)
	}
	if len(decoded) != 32 {
		return nil, fmt.Errorf("%s must decode to 32 bytes", name)
	}
	return decoded, nil
}

func isInvoiceNotFound(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unable to locate invoice") ||
		strings.Contains(msg, "invoice not found") ||
		strings.Contains(msg, "not found")
}
