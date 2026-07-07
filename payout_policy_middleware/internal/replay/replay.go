package replay

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"payout_policy_middleware/internal/httpapi"
	"payout_policy_middleware/internal/policy"
	"payout_policy_middleware/internal/robosats"
)

type Validator interface {
	ValidateLNPolicy(ctx context.Context, req httpapi.LNPayoutRequest, opts policy.LNOptions) (*robosats.LNRow, error)
	ValidateOnchainPolicy(ctx context.Context, req httpapi.OnchainPayoutRequest) (*robosats.OnchainRow, error)
	ValidateKeysendPolicy(ctx context.Context, req *httpapi.KeysendRequest) (*robosats.DevfundOrder, error)
}

type result struct {
	Kind     string `json:"kind"`
	Checked  int    `json:"checked"`
	Approved int    `json:"approved"`
	Rejected int    `json:"rejected"`
	Errors   int    `json:"errors"`
}

type aggregate struct {
	OK      bool     `json:"ok"`
	DryRun  bool     `json:"dryRun"`
	Results []result `json:"results"`
}

func Run(ctx context.Context, auditPool *pgxpool.Pool, validator Validator, args []string) error {
	fs := flag.NewFlagSet("replay-audit", flag.ContinueOnError)
	kind := fs.String("kind", "all", "audit kind to replay: all, ln, onchain, keysend")
	limit := fs.Int("limit", 100, "maximum rows per kind")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *limit < 1 {
		return fmt.Errorf("limit must be positive")
	}

	var results []result
	kinds := []string{strings.ToLower(*kind)}
	if *kind == "all" {
		kinds = []string{"ln", "onchain", "keysend"}
	}
	for _, selected := range kinds {
		switch selected {
		case "ln":
			res, err := replayLN(ctx, auditPool, validator, *limit)
			if err != nil {
				return err
			}
			results = append(results, res)
		case "onchain":
			res, err := replayOnchain(ctx, auditPool, validator, *limit)
			if err != nil {
				return err
			}
			results = append(results, res)
		case "keysend":
			res, err := replayKeysend(ctx, auditPool, validator, *limit)
			if err != nil {
				return err
			}
			results = append(results, res)
		default:
			return fmt.Errorf("unknown replay kind %q", selected)
		}
	}

	summary := aggregate{OK: true, DryRun: true, Results: results}
	for _, res := range results {
		if res.Errors > 0 {
			summary.OK = false
		}
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}

func replayLN(ctx context.Context, pool *pgxpool.Pool, validator Validator, limit int) (result, error) {
	res := result{Kind: "ln"}
	rows, err := pool.Query(ctx, `
		SELECT request_snapshot
		FROM payout_policy_attempts
		ORDER BY updated_at DESC
		LIMIT $1;`, limit)
	if err != nil {
		return res, err
	}
	defer rows.Close()
	for rows.Next() {
		res.Checked++
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			res.Errors++
			log.Printf("replay ln scan error: %v", err)
			continue
		}
		var req httpapi.LNPayoutRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			res.Errors++
			log.Printf("replay ln decode error: %v", err)
			continue
		}
		if _, err := validator.ValidateLNPolicy(ctx, req, policy.LNOptions{AllowTerminal: true, AllowExpired: true}); err != nil {
			res.Rejected++
			log.Printf("replay ln rejected request_id=%s order_id=%d reason=%v", req.RequestID, req.OrderID, err)
			continue
		}
		res.Approved++
	}
	return res, rows.Err()
}

func replayOnchain(ctx context.Context, pool *pgxpool.Pool, validator Validator, limit int) (result, error) {
	res := result{Kind: "onchain"}
	rows, err := pool.Query(ctx, `
		SELECT request_snapshot
		FROM payout_policy_onchain_attempts
		ORDER BY updated_at DESC
		LIMIT $1;`, limit)
	if err != nil {
		return res, err
	}
	defer rows.Close()
	for rows.Next() {
		res.Checked++
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			res.Errors++
			log.Printf("replay onchain scan error: %v", err)
			continue
		}
		var req httpapi.OnchainPayoutRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			res.Errors++
			log.Printf("replay onchain decode error: %v", err)
			continue
		}
		if _, err := validator.ValidateOnchainPolicy(ctx, req); err != nil {
			res.Rejected++
			log.Printf("replay onchain rejected request_id=%s order_id=%d reason=%v", req.RequestID, req.OrderID, err)
			continue
		}
		res.Approved++
	}
	return res, rows.Err()
}

func replayKeysend(ctx context.Context, pool *pgxpool.Pool, validator Validator, limit int) (result, error) {
	res := result{Kind: "keysend"}
	rows, err := pool.Query(ctx, `
		SELECT request_snapshot
		FROM payout_policy_keysend_attempts
		ORDER BY updated_at DESC
		LIMIT $1;`, limit)
	if err != nil {
		return res, err
	}
	defer rows.Close()
	for rows.Next() {
		res.Checked++
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			res.Errors++
			log.Printf("replay keysend scan error: %v", err)
			continue
		}
		var req httpapi.KeysendRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			res.Errors++
			log.Printf("replay keysend decode error: %v", err)
			continue
		}
		if _, err := validator.ValidateKeysendPolicy(ctx, &req); err != nil {
			res.Rejected++
			log.Printf("replay keysend rejected request_id=%s reason=%v", req.RequestID, err)
			continue
		}
		res.Approved++
	}
	return res, rows.Err()
}
