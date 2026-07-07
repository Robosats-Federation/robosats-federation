package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DatabaseURL      string
	AuditDatabaseURL string
	ServiceToken     string

	LNDGRPCHost       string
	LNDTLSCertPath    string
	LNDTLSCertBase64  string
	LNDMacaroonPath   string
	LNDMacaroonBase64 string
	LNDProtoPackage   string
	LNDProtoPath      string

	BitcoindRPCURL            string
	BitcoindRPCUser           string
	BitcoindRPCPassword       string
	BitcoindRPCTimeoutSeconds int
	RequireBitcoindValidation bool

	MaxPayoutSats                 int64
	MaxOnchainPayoutSats          int64
	MaxDevfundKeysendSats         int64
	MaxRewardWithdrawalSats       int64
	RewardRoutingFeeLimitPPM      int64
	RewardMinFlatFeeLimitSats     int64
	RewardWithdrawalCooldownHours int
	DevfundPubkey                 string
	DevfundKeysendTimeout         int64
	AllowSpendUnconfirmed         bool
	BitcoinNetwork                string
	StrictAddressNetwork          bool
	LNTrackTimeoutSeconds         int
	LNStreamTimeoutSeconds        int
	LNDAdapterRPCTimeoutSeconds   int
	LNDAdapterSlowTimeoutSeconds  int
	ReadyzCacheTTLSeconds         int
	LogContextCacheTTLSeconds     int
	HTTPReadHeaderTimeout         int
	HTTPReadTimeout               int
	HTTPWriteTimeout              int
	HTTPIdleTimeout               int
	ShutdownTimeout               int
	DBPoolMin                     int32
	DBPoolMax                     int32
	Host                          string
	Port                          int
}

func FromEnv() (Config, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	cfg := Config{
		DatabaseURL:                   databaseURL,
		AuditDatabaseURL:              envString("AUDIT_DATABASE_URL", databaseURL),
		ServiceToken:                  os.Getenv("ROBOSATS_SERVICE_TOKEN"),
		LNDGRPCHost:                   envString("LND_GRPC_HOST", "127.0.0.1:10009"),
		LNDTLSCertPath:                os.Getenv("LND_TLS_CERT"),
		LNDTLSCertBase64:              os.Getenv("LND_TLS_CERT_BASE64"),
		LNDMacaroonPath:               os.Getenv("LND_MACAROON"),
		LNDMacaroonBase64:             os.Getenv("LND_MACAROON_BASE64"),
		LNDProtoPackage:               os.Getenv("LND_PROTO_PACKAGE"),
		LNDProtoPath:                  os.Getenv("LND_PROTO_PATH"),
		BitcoindRPCURL:                os.Getenv("BITCOIND_RPCURL"),
		BitcoindRPCUser:               os.Getenv("BITCOIND_RPCUSER"),
		BitcoindRPCPassword:           os.Getenv("BITCOIND_RPCPASSWORD"),
		BitcoindRPCTimeoutSeconds:     envInt("BITCOIND_RPC_TIMEOUT_SECONDS", 10),
		RequireBitcoindValidation:     EnvBool("REQUIRE_BITCOIND_VALIDATION", true),
		MaxPayoutSats:                 int64(envInt("MAX_PAYOUT_SATS", 5_000_000)),
		MaxOnchainPayoutSats:          int64(envInt("MAX_ONCHAIN_PAYOUT_SATS", 5_000_000)),
		MaxDevfundKeysendSats:         int64(envInt("MAX_DEVFUND_KEYSEND_SATS", 5_000_000)),
		MaxRewardWithdrawalSats:       int64(envInt("MAX_REWARD_WITHDRAWAL_SATS", 5_000_000)),
		RewardRoutingFeeLimitPPM:      int64(envInt("REWARD_ROUTING_FEE_LIMIT_PPM", 1000)),
		RewardMinFlatFeeLimitSats:     int64(envInt("REWARD_MIN_FLAT_FEE_LIMIT_SATS", 2)),
		RewardWithdrawalCooldownHours: envInt("REWARD_WITHDRAWAL_COOLDOWN_HOURS", 48),
		DevfundPubkey:                 strings.ToLower(os.Getenv("DEVFUND_PUBKEY")),
		DevfundKeysendTimeout:         int64(envInt("DEVFUND_KEYSEND_TIMEOUT_SECONDS", 280)),
		AllowSpendUnconfirmed:         EnvBool("ALLOW_SPEND_UNCONFIRMED", false),
		BitcoinNetwork:                strings.ToLower(envString("BITCOIN_NETWORK", "mainnet")),
		StrictAddressNetwork:          EnvBool("STRICT_ADDRESS_NETWORK", true),
		LNTrackTimeoutSeconds:         envInt("LN_TRACK_TIMEOUT_SECONDS", 3),
		LNStreamTimeoutSeconds:        envInt("LN_STREAM_TIMEOUT_SECONDS", 160),
		LNDAdapterRPCTimeoutSeconds:   envInt("LND_ADAPTER_RPC_TIMEOUT_SECONDS", 5),
		LNDAdapterSlowTimeoutSeconds:  envInt("LND_ADAPTER_SLOW_TIMEOUT_SECONDS", 10),
		ReadyzCacheTTLSeconds:         envInt("READYZ_CACHE_TTL_SECONDS", 10),
		LogContextCacheTTLSeconds:     envInt("LOG_CONTEXT_CACHE_TTL_SECONDS", 30),
		HTTPReadHeaderTimeout:         envInt("HTTP_READ_HEADER_TIMEOUT_SECONDS", 10),
		HTTPReadTimeout:               envInt("HTTP_READ_TIMEOUT_SECONDS", 30),
		HTTPWriteTimeout:              envInt("HTTP_WRITE_TIMEOUT_SECONDS", 310),
		HTTPIdleTimeout:               envInt("HTTP_IDLE_TIMEOUT_SECONDS", 120),
		ShutdownTimeout:               envInt("SHUTDOWN_TIMEOUT_SECONDS", 30),
		DBPoolMin:                     int32(envInt("DB_POOL_MIN", 1)),
		DBPoolMax:                     int32(envInt("DB_POOL_MAX", 10)),
		Host:                          envString("HOST", "0.0.0.0"),
		Port:                          envInt("PORT", 8088),
	}
	if cfg.DBPoolMax < cfg.DBPoolMin {
		return Config{}, fmt.Errorf("DB_POOL_MAX must be >= DB_POOL_MIN")
	}
	return cfg, nil
}

func EnvBool(name string, defaultValue bool) bool {
	value, ok := os.LookupEnv(name)
	if !ok {
		return defaultValue
	}
	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func envString(name, defaultValue string) string {
	if value, ok := os.LookupEnv(name); ok {
		return value
	}
	return defaultValue
}

func envInt(name string, defaultValue int) int {
	value, ok := os.LookupEnv(name)
	if !ok || value == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		panic(fmt.Sprintf("%s must be an integer: %v", name, err))
	}
	return parsed
}

func ReadSecret(path, valueBase64, label string) ([]byte, error) {
	if path != "" {
		return os.ReadFile(path)
	}
	if valueBase64 != "" {
		return base64.StdEncoding.DecodeString(valueBase64)
	}
	return nil, fmt.Errorf("%s is not configured", label)
}
