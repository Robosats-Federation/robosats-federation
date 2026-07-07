package config

import "testing"

func TestEnvBool(t *testing.T) {
	t.Setenv("BOOL_TRUE", "yes")
	t.Setenv("BOOL_FALSE", "no")

	if !EnvBool("BOOL_TRUE", false) {
		t.Fatal("expected yes to parse as true")
	}
	if EnvBool("BOOL_FALSE", true) {
		t.Fatal("expected no to parse as false")
	}
	if !EnvBool("BOOL_MISSING", true) {
		t.Fatal("expected missing value to use default")
	}
}

func TestFromEnvPerformanceDefaults(t *testing.T) {
	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReadyzCacheTTLSeconds != 10 {
		t.Fatalf("unexpected readyz cache ttl: %d", cfg.ReadyzCacheTTLSeconds)
	}
	if cfg.LogContextCacheTTLSeconds != 30 {
		t.Fatalf("unexpected log context cache ttl: %d", cfg.LogContextCacheTTLSeconds)
	}
	if cfg.LNDAdapterRPCTimeoutSeconds != 5 {
		t.Fatalf("unexpected adapter rpc timeout: %d", cfg.LNDAdapterRPCTimeoutSeconds)
	}
	if cfg.LNDAdapterSlowTimeoutSeconds != 10 {
		t.Fatalf("unexpected adapter slow timeout: %d", cfg.LNDAdapterSlowTimeoutSeconds)
	}
}

func TestFromEnvPerformanceOverrides(t *testing.T) {
	t.Setenv("READYZ_CACHE_TTL_SECONDS", "12")
	t.Setenv("LOG_CONTEXT_CACHE_TTL_SECONDS", "45")
	t.Setenv("LND_ADAPTER_RPC_TIMEOUT_SECONDS", "4")
	t.Setenv("LND_ADAPTER_SLOW_TIMEOUT_SECONDS", "11")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReadyzCacheTTLSeconds != 12 ||
		cfg.LogContextCacheTTLSeconds != 45 ||
		cfg.LNDAdapterRPCTimeoutSeconds != 4 ||
		cfg.LNDAdapterSlowTimeoutSeconds != 11 {
		t.Fatalf("unexpected overrides: %#v", cfg)
	}
}
