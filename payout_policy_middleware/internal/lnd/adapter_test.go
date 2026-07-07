package lnd

import (
	"testing"

	"github.com/lightningnetwork/lnd/lnrpc"
)

func TestDummyFeeAddress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		info *lnrpc.GetInfoResponse
		want string
	}{
		{
			name: "testnet",
			info: &lnrpc.GetInfoResponse{Testnet: true},
			want: "tb1qehyqhruxwl2p5pt52k6nxj4v8wwc3f3pg7377x",
		},
		{
			name: "regtest",
			info: &lnrpc.GetInfoResponse{Chains: []*lnrpc.Chain{{Network: "regtest"}}},
			want: "bcrt1q3w8xja7knmycsglnxg2xzjq8uv9u7jdwau25nl",
		},
		{
			name: "mainnet default",
			info: &lnrpc.GetInfoResponse{Chains: []*lnrpc.Chain{{Network: "mainnet"}}},
			want: "bc1qgxwaqe4m9mypd7ltww53yv3lyxhcfnhzzvy5j3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := dummyFeeAddress(tt.info); got != tt.want {
				t.Fatalf("dummyFeeAddress() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDecodePayReqResponseMapsRouteHints(t *testing.T) {
	t.Parallel()

	got := decodePayReqResponse(&lnrpc.PayReq{
		Destination: "destination",
		PaymentHash: "hash",
		NumSatoshis: 42,
		Timestamp:   123,
		Expiry:      456,
		Description: "memo",
		CltvExpiry:  80,
		PaymentAddr: []byte{1, 2, 3},
		NumMsat:     42000,
		RouteHints: []*lnrpc.RouteHint{
			{HopHints: []*lnrpc.HopHint{
				{
					NodeId:                    "node",
					ChanId:                    7,
					FeeBaseMsat:               1,
					FeeProportionalMillionths: 2,
					CltvExpiryDelta:           3,
				},
			}},
		},
	})

	if got.PaymentHash != "hash" || got.NumSatoshis != 42 || got.PaymentAddr != "010203" {
		t.Fatalf("unexpected decoded response: %+v", got)
	}
	if len(got.RouteHints) != 1 || len(got.RouteHints[0].HopHints) != 1 {
		t.Fatalf("unexpected route hints: %+v", got.RouteHints)
	}
	hop := got.RouteHints[0].HopHints[0]
	if hop.NodeID != "node" || hop.ChanID != 7 || hop.CltvExpiryDelta != 3 {
		t.Fatalf("unexpected hop hint: %+v", hop)
	}
}

func TestLookupInvoiceResponse(t *testing.T) {
	t.Parallel()

	got := lookupInvoiceResponse("abc", &lnrpc.Invoice{
		Value:       100,
		AmtPaidSat:  100,
		AmtPaidMsat: 100000,
		State:       lnrpc.Invoice_ACCEPTED,
		Htlcs: []*lnrpc.InvoiceHTLC{
			{
				ChanId:       1,
				HtlcIndex:    2,
				AmtMsat:      100000,
				AcceptHeight: 10,
				ExpiryHeight: 99,
				State:        lnrpc.InvoiceHTLCState_ACCEPTED,
			},
			{
				ExpiryHeight: 88,
				State:        lnrpc.InvoiceHTLCState_SETTLED,
			},
		},
	})

	if !got.Found || got.State != "ACCEPTED" || got.StateCode != int32(lnrpc.Invoice_ACCEPTED) {
		t.Fatalf("unexpected invoice state: %+v", got)
	}
	if got.ExpiryHeight != 99 {
		t.Fatalf("expiry height = %d, want 99", got.ExpiryHeight)
	}
	if len(got.HTLCs) != 2 || got.HTLCs[0].State != "ACCEPTED" {
		t.Fatalf("unexpected htlcs: %+v", got.HTLCs)
	}
}

func TestDecode32Hex(t *testing.T) {
	t.Parallel()

	valid := "0000000000000000000000000000000000000000000000000000000000000000"
	if _, err := decode32Hex(valid, "payment_hash"); err != nil {
		t.Fatalf("decode32Hex(valid) returned error: %v", err)
	}
	if _, err := decode32Hex("00", "payment_hash"); err == nil {
		t.Fatal("decode32Hex(short) returned nil error")
	}
	if _, err := decode32Hex("zz", "payment_hash"); err == nil {
		t.Fatal("decode32Hex(non-hex) returned nil error")
	}
}
