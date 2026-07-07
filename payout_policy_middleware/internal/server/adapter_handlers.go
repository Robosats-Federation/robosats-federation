package server

import (
	"errors"
	"net/http"

	"payout_policy_middleware/internal/httpapi"
	"payout_policy_middleware/internal/policy"
)

func (s *Server) getVersion(w http.ResponseWriter, r *http.Request) {
	version, err := s.lnd.GetVersion(r.Context())
	if err != nil {
		writeAdapterError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, version)
}

func (s *Server) decodePayReq(w http.ResponseWriter, r *http.Request) {
	var req httpapi.DecodePayReqRequest
	if err := decodeObject(r, &req); err != nil {
		writeAdapterError(w, http.StatusBadRequest, err)
		return
	}
	if req.PaymentRequest == "" {
		writeAdapterError(w, http.StatusBadRequest, errors.New("payment_request is required"))
		return
	}
	decoded, err := s.lnd.DecodePayReqFull(r.Context(), req.PaymentRequest)
	if err != nil {
		writeAdapterError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, decoded)
}

func (s *Server) estimateFee(w http.ResponseWriter, r *http.Request) {
	var req httpapi.EstimateFeeRequest
	if err := decodeObject(r, &req); err != nil {
		writeAdapterError(w, http.StatusBadRequest, err)
		return
	}
	setLogField(r, "adapter_op", "estimate_fee")
	setLogFieldInt64(r, "amount_sats", req.AmountSats)
	fee, err := s.lnd.EstimateFee(r.Context(), req)
	if err != nil {
		writeAdapterError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, fee)
}

func (s *Server) walletBalance(w http.ResponseWriter, r *http.Request) {
	balance, err := s.lnd.WalletBalance(r.Context())
	if err != nil {
		writeAdapterError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, balance)
}

func (s *Server) channelBalance(w http.ResponseWriter, r *http.Request) {
	balance, err := s.lnd.ChannelBalance(r.Context())
	if err != nil {
		writeAdapterError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, balance)
}

func (s *Server) addHoldInvoice(w http.ResponseWriter, r *http.Request) {
	var req httpapi.AddHoldInvoiceRequest
	if err := decodeObject(r, &req); err != nil {
		writeAdapterError(w, http.StatusBadRequest, err)
		return
	}
	setLogField(r, "adapter_op", "add_hold_invoice")
	setLogFieldInt64(r, "amount_sats", req.AmountSats)
	invoice, err := s.lnd.AddHoldInvoice(r.Context(), req)
	if err != nil {
		writeAdapterError(w, http.StatusInternalServerError, err)
		return
	}
	setLogField(r, "payment_hash", invoice.PaymentHash)
	setLogField(r, "invoice_type", "HOLD")
	writeJSON(w, http.StatusOK, invoice)
}

func (s *Server) cancelInvoice(w http.ResponseWriter, r *http.Request) {
	var req httpapi.PaymentHashRequest
	if err := decodeObject(r, &req); err != nil {
		writeAdapterError(w, http.StatusBadRequest, err)
		return
	}
	if req.PaymentHash == "" {
		writeAdapterError(w, http.StatusBadRequest, errors.New("payment_hash is required"))
		return
	}
	setLogField(r, "adapter_op", "cancel_invoice")
	s.annotateLNPaymentByHash(r, req.PaymentHash)
	if err := s.lnd.CancelInvoice(r.Context(), req.PaymentHash); err != nil {
		writeAdapterError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, httpapi.OperationResponse{Ok: true, Message: "ok"})
}

func (s *Server) settleInvoice(w http.ResponseWriter, r *http.Request) {
	var req httpapi.PreimageRequest
	if err := decodeObject(r, &req); err != nil {
		writeAdapterError(w, http.StatusBadRequest, err)
		return
	}
	if req.Preimage == "" {
		writeAdapterError(w, http.StatusBadRequest, errors.New("preimage is required"))
		return
	}
	setLogField(r, "adapter_op", "settle_invoice")
	s.annotateLNPaymentByPreimage(r, req.Preimage)
	if err := s.lnd.SettleInvoice(r.Context(), req.Preimage); err != nil {
		writeAdapterError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, httpapi.OperationResponse{Ok: true, Message: "ok"})
}

func (s *Server) lookupInvoice(w http.ResponseWriter, r *http.Request) {
	var req httpapi.PaymentHashRequest
	if err := decodeObject(r, &req); err != nil {
		writeAdapterError(w, http.StatusBadRequest, err)
		return
	}
	if req.PaymentHash == "" {
		writeAdapterError(w, http.StatusBadRequest, errors.New("payment_hash is required"))
		return
	}
	setLogField(r, "adapter_op", "lookup_invoice")
	setLogField(r, "payment_hash", req.PaymentHash)
	invoice, err := s.lnd.LookupInvoice(r.Context(), req.PaymentHash)
	if err != nil {
		s.annotateLNPaymentByHash(r, req.PaymentHash)
		writeAdapterError(w, http.StatusInternalServerError, err)
		return
	}
	setLogField(r, "invoice_state", invoice.State)
	if !invoice.Found {
		s.annotateLNPaymentByHash(r, req.PaymentHash)
	}
	writeJSON(w, http.StatusOK, invoice)
}

func writeAdapterError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, policy.InternalError(err))
}
