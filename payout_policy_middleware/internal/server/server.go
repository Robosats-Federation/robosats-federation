package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"payout_policy_middleware/internal/audit"
	"payout_policy_middleware/internal/bitcoin"
	"payout_policy_middleware/internal/config"
	"payout_policy_middleware/internal/db"
	"payout_policy_middleware/internal/httpapi"
	"payout_policy_middleware/internal/lnd"
	"payout_policy_middleware/internal/policy"
	"payout_policy_middleware/internal/robosats"
)

type Server struct {
	cfg         config.Config
	pools       *db.Pools
	robosats    *robosats.Repository
	validator   *policy.Validator
	lnd         lnd.Interface
	audit       *audit.Repository
	bitcoin     *bitcoin.Client
	readyzCache readinessCache
	logCtx      *lnPaymentLogContextCache
}

type readyzPayload struct {
	OK     bool            `json:"ok"`
	Checks map[string]bool `json:"checks"`
}

type readinessCache struct {
	mu        sync.Mutex
	expiresAt time.Time
	status    int
	payload   readyzPayload
}

func (c *readinessCache) get() (int, readyzPayload, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.expiresAt.IsZero() || time.Now().After(c.expiresAt) {
		return 0, readyzPayload{}, false
	}
	return c.status, cloneReadyzPayload(c.payload), true
}

func (c *readinessCache) set(ttl time.Duration, status int, payload readyzPayload) {
	if ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status = status
	c.payload = cloneReadyzPayload(payload)
	c.expiresAt = time.Now().Add(ttl)
}

func cloneReadyzPayload(payload readyzPayload) readyzPayload {
	checks := make(map[string]bool, len(payload.Checks))
	for key, value := range payload.Checks {
		checks[key] = value
	}
	return readyzPayload{OK: payload.OK, Checks: checks}
}

func New(cfg config.Config, pools *db.Pools, validator *policy.Validator, lndClient lnd.Interface, auditRepo *audit.Repository, bitcoinClient *bitcoin.Client) *Server {
	return &Server{
		cfg:       cfg,
		pools:     pools,
		robosats:  robosats.NewRepository(pools.RoboSats),
		validator: validator,
		lnd:       lndClient,
		audit:     auditRepo,
		bitcoin:   bitcoinClient,
		logCtx:    newLNPaymentLogContextCache(time.Duration(cfg.LogContextCacheTTLSeconds)*time.Second, 2048),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /readyz", s.readyz)
	mux.HandleFunc("POST /v1/robosats/lnd/send-payment-v2", s.sendPaymentV2)
	mux.HandleFunc("POST /v1/robosats/lnd/send-coins", s.sendCoins)
	mux.HandleFunc("POST /v1/robosats/lnd/send-keysend", s.sendKeysend)
	mux.HandleFunc("POST /v1/robosats/lnd/send-reward", s.sendReward)
	mux.HandleFunc("POST /v1/robosats/rewards/source", s.recordRewardSource)
	mux.HandleFunc("GET /v1/robosats/lnd/version", s.getVersion)
	mux.HandleFunc("POST /v1/robosats/lnd/decode-payreq", s.decodePayReq)
	mux.HandleFunc("POST /v1/robosats/lnd/estimate-fee", s.estimateFee)
	mux.HandleFunc("GET /v1/robosats/lnd/wallet-balance", s.walletBalance)
	mux.HandleFunc("GET /v1/robosats/lnd/channel-balance", s.channelBalance)
	mux.HandleFunc("POST /v1/robosats/lnd/hold-invoice", s.addHoldInvoice)
	mux.HandleFunc("POST /v1/robosats/lnd/cancel-invoice", s.cancelInvoice)
	mux.HandleFunc("POST /v1/robosats/lnd/settle-invoice", s.settleInvoice)
	mux.HandleFunc("POST /v1/robosats/lnd/lookup-invoice", s.lookupInvoice)
	return requestLogger(s.auth(mux))
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		if s.cfg.ServiceToken == "" {
			writeJSON(w, http.StatusInternalServerError, policy.InternalError(errors.New("ROBOSATS_SERVICE_TOKEN is not configured")))
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+s.cfg.ServiceToken {
			writeJSON(w, http.StatusUnauthorized, policy.Reject("invalid service token"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	if status, payload, ok := s.readyzCache.get(); ok {
		setLogField(r, "readyz_cached", true)
		writeJSON(w, status, payload)
		return
	}

	status, payload := s.computeReadyz(r)
	s.readyzCache.set(time.Duration(s.cfg.ReadyzCacheTTLSeconds)*time.Second, status, payload)
	writeJSON(w, status, payload)
}

func (s *Server) computeReadyz(r *http.Request) (int, readyzPayload) {
	ctx := r.Context()
	checks := map[string]bool{
		"postgres":       false,
		"audit_postgres": false,
		"audit_tables":   false,
		"bitcoind":       false,
	}
	if _, err := s.pools.RoboSats.Exec(ctx, "SELECT 1"); err != nil {
		log.Printf("RoboSats Postgres readiness check failed: %v", err)
	} else {
		checks["postgres"] = true
	}
	if _, err := s.pools.Audit.Exec(ctx, "SELECT 1"); err != nil {
		log.Printf("Audit Postgres readiness check failed: %v", err)
	} else {
		checks["audit_postgres"] = true
	}
	var tableCount int
	err := s.pools.Audit.QueryRow(ctx, `
                    SELECT COUNT(*) AS table_count
                    FROM information_schema.tables
                    WHERE table_schema = 'public'
                      AND table_name IN (
                        'payout_policy_attempts',
                        'payout_policy_onchain_attempts',
                        'payout_policy_keysend_attempts',
                        'payout_policy_reward_sources',
                        'payout_policy_reward_attempts'
                      );`).Scan(&tableCount)
	if err != nil {
		log.Printf("Audit table readiness check failed: %v", err)
	} else {
		checks["audit_tables"] = tableCount == 5
	}
	var info struct {
		Chain string `json:"chain"`
	}
	if err := s.bitcoin.RPC(ctx, "getblockchaininfo", nil, &info); err != nil {
		log.Printf("bitcoind readiness check failed: %v", err)
	} else {
		checks["bitcoind"] = true
	}
	ok := true
	for _, value := range checks {
		ok = ok && value
	}
	status := http.StatusOK
	if !ok {
		status = http.StatusServiceUnavailable
	}
	return status, readyzPayload{OK: ok, Checks: checks}
}

func (s *Server) sendPaymentV2(w http.ResponseWriter, r *http.Request) {
	var req httpapi.LNPayoutRequest
	if err := decodeObject(r, &req); err != nil {
		writeJSON(w, http.StatusOK, policy.Reject(err.Error()))
		return
	}
	annotateLNPayoutRequest(r, req)
	s.annotateLNPaymentByHash(r, req.PaymentHash)
	ctx := r.Context()
	row, err := s.validator.ValidateLNPolicy(ctx, req, policy.LNOptions{AllowTerminal: true, AllowExpired: true})
	if err != nil {
		s.handleLNError(w, ctx, req, err)
		return
	}
	tracked, err := s.lnd.TryTrackPayment(ctx, req.PaymentHash)
	if err != nil {
		s.handleLNInternalError(w, ctx, req, err)
		return
	}
	if tracked != nil && containsString([]string{"IN_FLIGHT", "INITIATED", "ALREADY_PAID"}, tracked.Status) {
		s.safeAuditLN(ctx, req, *tracked, nil)
		writeJSON(w, http.StatusOK, tracked)
		return
	}
	if row.OrderStatus == 14 || row.PayoutStatus == 8 {
		s.handleLNInternalError(w, ctx, req, errors.New("ln payout is already terminal in RoboSats DB but not confirmed by LND"))
		return
	}
	if row.PolicyInvoiceExpired {
		result := lnExpired(req, row.PolicyInvoiceExpiredReason)
		reason := result.Message
		s.safeAuditLN(ctx, req, result, &reason)
		writeJSON(w, http.StatusOK, result)
		return
	}
	result, err := s.lnd.SendPayment(ctx, req)
	if err != nil {
		s.handleLNInternalError(w, ctx, req, err)
		return
	}
	s.safeAuditLN(ctx, req, *result, nil)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) sendCoins(w http.ResponseWriter, r *http.Request) {
	var req httpapi.OnchainPayoutRequest
	if err := decodeObject(r, &req); err != nil {
		writeJSON(w, http.StatusOK, policy.Reject(err.Error()))
		return
	}
	annotateOnchainPayoutRequest(r, req)
	ctx := r.Context()
	onchainPaymentID := req.OnchainPaymentID
	if _, err := s.validator.ValidateOnchainPolicy(ctx, req); err != nil {
		if isPolicyError(err) {
			writeJSON(w, http.StatusOK, policy.Reject(err.Error()))
			return
		}
		result := policy.InternalError(err)
		if onchainPaymentID != 0 {
			reason := err.Error()
			if auditErr := s.audit.UpdateOnchainAttempt(ctx, onchainPaymentID, result, &reason); auditErr != nil {
				log.Printf("On-chain audit update failed: %v", auditErr)
			}
		}
		log.Printf("On-chain payout middleware error: %v", err)
		writeJSON(w, http.StatusInternalServerError, result)
		return
	}
	inserted, existing, err := s.audit.ReserveOnchainAttempt(ctx, req)
	if err != nil {
		s.handleOnchainInternalError(w, ctx, onchainPaymentID, err)
		return
	}
	if !inserted {
		sameRequest := existing != nil &&
			existing.Address == req.Address &&
			existing.AmountSats == req.AmountSats &&
			existing.FeeRateSatVByte == req.FeeRateSatVByte
		if sameRequest && existing.TxID.Valid && existing.TxID.String != "" {
			writeJSON(w, http.StatusOK, httpapi.OnchainResponse{
				Decision:         "APPROVED",
				Status:           "ALREADY_BROADCASTED",
				OrderID:          req.OrderID,
				OnchainPaymentID: onchainPaymentID,
				TxID:             existing.TxID.String,
				Message:          "already broadcasted",
			})
			return
		}
		writeJSON(w, http.StatusOK, policy.Reject("on-chain payout already has a middleware attempt; reconcile before retry"))
		return
	}
	result, err := s.lnd.SendCoins(ctx, req)
	if err != nil {
		s.handleOnchainInternalError(w, ctx, onchainPaymentID, err)
		return
	}
	if result.TxID == "" {
		s.handleOnchainInternalError(w, ctx, onchainPaymentID, errors.New("LND SendCoins returned empty txid"))
		return
	}
	if err := s.audit.UpdateOnchainAttempt(ctx, onchainPaymentID, result, nil); err != nil {
		log.Printf("On-chain audit update failed: %v", err)
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) sendKeysend(w http.ResponseWriter, r *http.Request) {
	var req httpapi.KeysendRequest
	if err := decodeObject(r, &req); err != nil {
		writeJSON(w, http.StatusOK, policy.Reject(err.Error()))
		return
	}
	annotateKeysendRequest(r, req)
	ctx := r.Context()
	row, err := s.validator.ValidateKeysendPolicy(ctx, &req)
	if err != nil {
		if isPolicyError(err) {
			writeJSON(w, http.StatusOK, policy.Reject(err.Error()))
			return
		}
		log.Printf("Keysend payout middleware error: %v", err)
		writeJSON(w, http.StatusInternalServerError, policy.InternalError(err))
		return
	}
	orderID, _, _, err := policy.ParseDevfundMessage(req.Message)
	if err != nil {
		writeJSON(w, http.StatusOK, policy.Reject(err.Error()))
		return
	}
	if row.OrderID != 0 {
		req.OrderID = row.OrderID
	} else {
		req.OrderID = orderID
	}
	setLogFieldInt64(r, "order_id", req.OrderID)
	requestID := req.RequestID
	inserted := false
	preimage, paymentHash, err := s.lnd.GenerateKeysendSecret()
	if err != nil {
		log.Printf("Keysend payout middleware error: %v", err)
		writeJSON(w, http.StatusInternalServerError, policy.InternalError(err))
		return
	}
	inserted, existing, err := s.audit.ReserveKeysendAttempt(ctx, req, preimage, paymentHash)
	if err != nil {
		log.Printf("Keysend payout middleware error: %v", err)
		writeJSON(w, http.StatusInternalServerError, policy.InternalError(err))
		return
	}
	if !inserted {
		sameRequest := existing != nil &&
			existing.TargetPubkey == req.TargetPubkey &&
			existing.Message == req.Message &&
			existing.AmountSats == req.AmountSats &&
			existing.FeeLimitSat == req.FeeLimitSat &&
			existing.TimeoutSeconds == req.TimeoutSeconds
		if sameRequest && containsString([]string{"SUCCEEDED", "ALREADY_PAID", "FAILED", "ERROR"}, existing.NormalizedStatus) {
			result := audit.KeysendAttemptResult(existing)
			writeJSON(w, http.StatusOK, result)
			return
		}
		if sameRequest {
			tracked, err := s.lnd.TryTrackPayment(ctx, existing.PaymentHash)
			if err != nil {
				log.Printf("Keysend track error: %v", err)
				writeJSON(w, http.StatusInternalServerError, policy.InternalError(err))
				return
			}
			if tracked != nil {
				tracked.PaymentHash = existing.PaymentHash
				tracked.PaymentPreimage = existing.Preimage
				if err := s.audit.UpdateKeysendAttempt(ctx, requestID, *tracked, nil); err != nil {
					log.Printf("Keysend audit update failed: %v", err)
				}
				writeJSON(w, http.StatusOK, tracked)
				return
			}
		}
		writeJSON(w, http.StatusOK, policy.Reject("keysend request already has a middleware attempt; reconcile before retry"))
		return
	}
	result, err := s.lnd.SendKeysend(ctx, req, preimage, paymentHash)
	if err != nil {
		if inserted && requestID != "" {
			reason := err.Error()
			if auditErr := s.audit.UpdateKeysendAttempt(ctx, requestID, httpapi.PaymentResponse{Decision: "ERROR", Status: "ERROR", Message: err.Error()}, &reason); auditErr != nil {
				log.Printf("Keysend audit update failed: %v", auditErr)
			}
		}
		log.Printf("Keysend payout middleware error: %v", err)
		writeJSON(w, http.StatusInternalServerError, policy.InternalError(err))
		return
	}
	if err := s.audit.UpdateKeysendAttempt(ctx, requestID, *result, nil); err != nil {
		log.Printf("Keysend audit update failed: %v", err)
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleLNError(w http.ResponseWriter, ctx context.Context, req httpapi.LNPayoutRequest, err error) {
	reason := err.Error()
	var result httpapi.PaymentResponse
	if strings.Contains(reason, "invoice expired") {
		result = lnExpired(req, reason)
	} else {
		result = httpapi.PaymentResponse{Decision: "REJECTED", Status: "POLICY_REJECTED", Message: reason}
	}
	s.safeAuditLN(ctx, req, result, &reason)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleLNInternalError(w http.ResponseWriter, ctx context.Context, req httpapi.LNPayoutRequest, err error) {
	result := httpapi.PaymentResponse{Decision: "ERROR", Status: "ERROR", PaymentHash: req.PaymentHash, Message: err.Error()}
	reason := err.Error()
	s.safeAuditLN(ctx, req, result, &reason)
	log.Printf("LN payout middleware error: %v", err)
	writeJSON(w, http.StatusInternalServerError, result)
}

func (s *Server) recordRewardSource(w http.ResponseWriter, r *http.Request) {
	var req httpapi.RewardSourceRequest
	if err := decodeObject(r, &req); err != nil {
		writeJSON(w, http.StatusOK, policy.Reject(err.Error()))
		return
	}
	normalizeRewardSourceRequest(&req)
	annotateRewardSourceRequest(r, req)
	ctx := r.Context()
	if err := s.validator.ValidateRewardSource(ctx, req); err != nil {
		if isPolicyError(err) {
			writeJSON(w, http.StatusOK, policy.Reject(err.Error()))
			return
		}
		log.Printf("Reward source middleware error: %v", err)
		writeJSON(w, http.StatusInternalServerError, policy.InternalError(err))
		return
	}
	if err := s.audit.RecordRewardSource(ctx, req); err != nil {
		log.Printf("Reward source audit write failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, policy.InternalError(err))
		return
	}
	writeJSON(w, http.StatusOK, httpapi.OperationResponse{Ok: true, Message: "recorded"})
}

func (s *Server) sendReward(w http.ResponseWriter, r *http.Request) {
	var req httpapi.RewardWithdrawalRequest
	if err := decodeObject(r, &req); err != nil {
		writeJSON(w, http.StatusOK, policy.Reject(err.Error()))
		return
	}
	annotateRewardWithdrawalRequest(r, req)
	s.annotateLNPaymentByHash(r, req.PaymentHash)
	ctx := r.Context()
	row, err := s.validator.ValidateRewardWithdrawal(ctx, req)
	lnReq := rewardAsLNRequest(req, 0)
	if err != nil {
		reason := err.Error()
		var result httpapi.PaymentResponse
		if strings.Contains(reason, "invoice expired") {
			code := int64(4)
			result = httpapi.PaymentResponse{
				Decision:          "APPROVED",
				Status:            "EXPIRED",
				PaymentHash:       req.PaymentHash,
				FailureReasonCode: &code,
				Message:           reason,
			}
		} else {
			result = httpapi.PaymentResponse{Decision: "REJECTED", Status: "POLICY_REJECTED", Message: reason}
		}
		s.safeAuditLN(ctx, lnReq, result, &reason)
		writeJSON(w, http.StatusOK, result)
		return
	}
	lnReq = rewardAsLNRequest(req, row.SourceOrderID)
	setLogFieldInt64(r, "order_id", row.SourceOrderID)
	inserted, existing, err := s.audit.ReserveRewardAttempt(ctx, lnReq, row)
	if err != nil {
		result := httpapi.PaymentResponse{Decision: "ERROR", Status: "ERROR", PaymentHash: req.PaymentHash, Message: err.Error()}
		reason := err.Error()
		s.safeAuditLN(ctx, lnReq, result, &reason)
		log.Printf("Reward payout middleware error: %v", err)
		writeJSON(w, http.StatusInternalServerError, result)
		return
	}
	if !inserted {
		samePayment := existing != nil &&
			existing.PaymentHash == req.PaymentHash &&
			existing.AmountSats == req.AmountSats &&
			existing.FeeLimitSat == req.FeeLimitSat &&
			existing.TimeoutSeconds == req.TimeoutSeconds
		if samePayment && containsString([]string{"SUCCEEDED", "ALREADY_PAID", "FAILED", "ERROR", "EXPIRED"}, existing.NormalizedStatus) {
			result := audit.RewardAttemptResult(existing)
			writeJSON(w, http.StatusOK, result)
			return
		}
		if samePayment {
			tracked, err := s.lnd.TryTrackPayment(ctx, existing.PaymentHash)
			if err != nil {
				result := httpapi.PaymentResponse{Decision: "ERROR", Status: "ERROR", PaymentHash: req.PaymentHash, Message: err.Error()}
				reason := err.Error()
				if auditErr := s.audit.UpdateRewardAttempt(ctx, existing.RequestID, result, &reason); auditErr != nil {
					log.Printf("Reward audit update failed: %v", auditErr)
				}
				log.Printf("Reward track error: %v", err)
				writeJSON(w, http.StatusInternalServerError, result)
				return
			}
			if tracked != nil {
				if err := s.audit.UpdateRewardAttempt(ctx, existing.RequestID, *tracked, nil); err != nil {
					log.Printf("Reward audit update failed: %v", err)
				}
				writeJSON(w, http.StatusOK, tracked)
				return
			}
		}
		writeJSON(w, http.StatusOK, policy.Reject("reward withdrawal is temporarily unavailable"))
		return
	}
	tracked, err := s.lnd.TryTrackPayment(ctx, req.PaymentHash)
	if err != nil {
		result := httpapi.PaymentResponse{Decision: "ERROR", Status: "ERROR", PaymentHash: req.PaymentHash, Message: err.Error()}
		reason := err.Error()
		if auditErr := s.audit.UpdateRewardAttempt(ctx, lnReq.RequestID, result, &reason); auditErr != nil {
			log.Printf("Reward audit update failed: %v", auditErr)
		}
		s.safeAuditLN(ctx, lnReq, result, &reason)
		log.Printf("Reward payout middleware error: %v", err)
		writeJSON(w, http.StatusInternalServerError, result)
		return
	}
	if tracked != nil && containsString([]string{"IN_FLIGHT", "INITIATED", "ALREADY_PAID"}, tracked.Status) {
		if err := s.audit.UpdateRewardAttempt(ctx, lnReq.RequestID, *tracked, nil); err != nil {
			log.Printf("Reward audit update failed: %v", err)
		}
		s.safeAuditLN(ctx, lnReq, *tracked, nil)
		writeJSON(w, http.StatusOK, tracked)
		return
	}
	result, err := s.lnd.SendPayment(ctx, lnReq)
	if err != nil {
		result := httpapi.PaymentResponse{Decision: "ERROR", Status: "ERROR", PaymentHash: req.PaymentHash, Message: err.Error()}
		reason := err.Error()
		if auditErr := s.audit.UpdateRewardAttempt(ctx, lnReq.RequestID, result, &reason); auditErr != nil {
			log.Printf("Reward audit update failed: %v", auditErr)
		}
		s.safeAuditLN(ctx, lnReq, result, &reason)
		log.Printf("Reward payout middleware error: %v", err)
		writeJSON(w, http.StatusInternalServerError, result)
		return
	}
	if err := s.audit.UpdateRewardAttempt(ctx, lnReq.RequestID, *result, nil); err != nil {
		log.Printf("Reward audit update failed: %v", err)
	}
	s.safeAuditLN(ctx, lnReq, *result, nil)
	writeJSON(w, http.StatusOK, result)
}

func rewardAsLNRequest(req httpapi.RewardWithdrawalRequest, sourceOrderID int64) httpapi.LNPayoutRequest {
	return httpapi.LNPayoutRequest{
		RequestID:         req.RequestID,
		Source:            req.Source,
		Kind:              req.Kind,
		OrderID:           sourceOrderID,
		PaymentHash:       req.PaymentHash,
		PaymentRequest:    req.PaymentRequest,
		AmountSats:        req.AmountSats,
		FeeLimitSat:       req.FeeLimitSat,
		TimeoutSeconds:    req.TimeoutSeconds,
		RoutingBudgetPPM:  req.RoutingBudgetPPM,
		RoutingBudgetSats: req.RoutingBudgetSats,
		ReceiverUserID:    req.ReceiverUserID,
	}
}

func normalizeRewardSourceRequest(req *httpapi.RewardSourceRequest) {
	req.RequestID = strings.TrimSpace(req.RequestID)
	req.Source = strings.TrimSpace(req.Source)
	req.Kind = strings.TrimSpace(req.Kind)
	req.ReceiverRobotName = strings.TrimSpace(req.ReceiverRobotName)
	req.SlashedBondPaymentHash = strings.ToLower(strings.TrimSpace(req.SlashedBondPaymentHash))
	req.StakedBondPaymentHash = strings.ToLower(strings.TrimSpace(req.StakedBondPaymentHash))
	req.Reason = strings.TrimSpace(req.Reason)
}

func (s *Server) handleOnchainInternalError(w http.ResponseWriter, ctx context.Context, onchainPaymentID int64, err error) {
	result := policy.InternalError(err)
	if onchainPaymentID != 0 {
		reason := err.Error()
		if auditErr := s.audit.UpdateOnchainAttempt(ctx, onchainPaymentID, result, &reason); auditErr != nil {
			log.Printf("On-chain audit update failed: %v", auditErr)
		}
	}
	log.Printf("On-chain payout middleware error: %v", err)
	writeJSON(w, http.StatusInternalServerError, result)
}

func (s *Server) safeAuditLN(ctx context.Context, req httpapi.LNPayoutRequest, result httpapi.PaymentResponse, rejectionReason *string) {
	if err := s.audit.AuditLN(ctx, req, result, rejectionReason); err != nil {
		log.Printf("LN audit write failed: %v", err)
	}
}

func lnExpired(req httpapi.LNPayoutRequest, reason string) httpapi.PaymentResponse {
	code := int64(4)
	return httpapi.PaymentResponse{
		Decision:          "APPROVED",
		Status:            "EXPIRED",
		PaymentHash:       req.PaymentHash,
		FailureReasonCode: &code,
		Message:           reason,
	}
}

func decodeObject(r *http.Request, out any) error {
	var raw map[string]json.RawMessage
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r.Body); err != nil {
		return err
	}
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil || raw == nil {
		return errors.New("request body must be a JSON object")
	}
	return json.Unmarshal(buf.Bytes(), out)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("write JSON response failed: %v", err)
	}
}

func isPolicyError(err error) bool {
	var policyErr policy.PolicyError
	return errors.As(err, &policyErr)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
