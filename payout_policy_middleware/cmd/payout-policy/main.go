package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"payout_policy_middleware/internal/audit"
	"payout_policy_middleware/internal/bitcoin"
	"payout_policy_middleware/internal/config"
	"payout_policy_middleware/internal/db"
	"payout_policy_middleware/internal/lnd"
	"payout_policy_middleware/internal/migrate"
	"payout_policy_middleware/internal/policy"
	"payout_policy_middleware/internal/replay"
	"payout_policy_middleware/internal/robosats"
	"payout_policy_middleware/internal/server"
)

func main() {
	cfg, err := config.FromEnv()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	if len(os.Args) > 1 && os.Args[1] == "readyz-check" {
		if err := readyzCheck(cfg); err != nil {
			log.Fatal(err)
		}
		return
	}

	ctx := context.Background()
	pools, err := db.NewPools(ctx, cfg)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pools.Close()

	migrationPath := filepath.Join("migrations", "001_payout_policy_audit.sql")
	if err := migrate.ApplySQLFile(ctx, pools.Audit, migrationPath); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	lndClient, err := lnd.New(ctx, cfg)
	if err != nil {
		log.Fatalf("lnd: %v", err)
	}
	defer lndClient.Close()

	bitcoinClient := bitcoin.New(cfg)
	roboRepo := robosats.NewRepository(pools.RoboSats)
	auditRepo := audit.NewRepository(pools.Audit, cfg)
	validator := policy.NewValidator(cfg, roboRepo, auditRepo, lndClient, bitcoinClient)

	if len(os.Args) > 1 && os.Args[1] == "replay-audit" {
		if err := replay.Run(ctx, pools.Audit, validator, os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}

	app := server.New(cfg, pools, validator, lndClient, auditRepo, bitcoinClient)
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           app.Handler(),
		ReadHeaderTimeout: time.Duration(cfg.HTTPReadHeaderTimeout) * time.Second,
		ReadTimeout:       time.Duration(cfg.HTTPReadTimeout) * time.Second,
		WriteTimeout:      time.Duration(cfg.HTTPWriteTimeout) * time.Second,
		IdleTimeout:       time.Duration(cfg.HTTPIdleTimeout) * time.Second,
	}

	log.Printf("payout policy middleware listening on %s", addr)
	errc := make(chan error, 1)
	go func() {
		errc <- httpServer.ListenAndServe()
	}()

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errc:
		if err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	case sig := <-sigc:
		log.Printf("received signal %s; shutting down", sig)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.ShutdownTimeout)*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Fatal(err)
		}
	}
}

func readyzCheck(cfg config.Config) error {
	if cfg.ServiceToken == "" {
		return fmt.Errorf("ROBOSATS_SERVICE_TOKEN is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/readyz", cfg.Port), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.ServiceToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("readyz returned %s", resp.Status)
	}
	return nil
}
