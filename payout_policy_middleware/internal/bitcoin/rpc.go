package bitcoin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"payout_policy_middleware/internal/config"
	"payout_policy_middleware/internal/policy"
)

type Client struct {
	cfg    config.Config
	client *http.Client
}

func New(cfg config.Config) *Client {
	return &Client{
		cfg: cfg,
		client: &http.Client{
			Timeout: time.Duration(cfg.BitcoindRPCTimeoutSeconds) * time.Second,
		},
	}
}

func (c *Client) RPC(ctx context.Context, method string, params []any, result any) error {
	if c.cfg.BitcoindRPCURL == "" {
		return fmt.Errorf("BITCOIND_RPCURL is not configured")
	}
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      "payout-policy",
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BitcoindRPCURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.cfg.BitcoindRPCUser, c.cfg.BitcoindRPCPassword)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("bitcoind RPC HTTP %s", resp.Status)
	}
	var payload struct {
		Result json.RawMessage `json:"result"`
		Error  any             `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return err
	}
	if payload.Error != nil {
		return fmt.Errorf("%v", payload.Error)
	}
	if result != nil {
		return json.Unmarshal(payload.Result, result)
	}
	return nil
}

func (c *Client) AssertExpectedChain(ctx context.Context) error {
	if !c.cfg.StrictAddressNetwork {
		return nil
	}
	var info struct {
		Chain string `json:"chain"`
	}
	if err := c.RPC(ctx, "getblockchaininfo", nil, &info); err != nil {
		return err
	}
	expected := map[string]string{
		"mainnet": "main",
		"main":    "main",
		"testnet": "test",
		"test":    "test",
		"signet":  "signet",
		"regtest": "regtest",
	}[c.cfg.BitcoinNetwork]
	if expected == "" {
		expected = c.cfg.BitcoinNetwork
	}
	if info.Chain != expected {
		return policy.NewError(fmt.Sprintf("bitcoind chain %s does not match %s", info.Chain, c.cfg.BitcoinNetwork))
	}
	return nil
}

func (c *Client) ValidateAddress(ctx context.Context, address string) error {
	if !c.cfg.RequireBitcoindValidation {
		return nil
	}
	if err := c.AssertExpectedChain(ctx); err != nil {
		return err
	}
	var validation struct {
		IsValid bool `json:"isvalid"`
	}
	if err := c.RPC(ctx, "validateaddress", []any{address}, &validation); err != nil {
		return err
	}
	if !validation.IsValid {
		return policy.NewError("invalid bitcoin address")
	}
	return nil
}
