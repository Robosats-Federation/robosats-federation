package server

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestLoggerIncludesAnnotatedFields(t *testing.T) {
	var logs bytes.Buffer
	originalOutput := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(originalOutput)
		log.SetFlags(originalFlags)
	}()

	handler := requestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setLogField(r, "robot", "HelpfulVeranda735")
		setLogField(r, "invoice_concept", "TAKEBOND")
		setLogFieldInt64(r, "order_id", 89895)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/robosats/lnd/cancel-invoice", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	line := logs.String()
	for _, want := range []string{
		"method=POST",
		"path=/v1/robosats/lnd/cancel-invoice",
		"invoice_concept=TAKEBOND",
		"order_id=89895",
		"robot=HelpfulVeranda735",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("log line missing %q: %s", want, line)
		}
	}
}
