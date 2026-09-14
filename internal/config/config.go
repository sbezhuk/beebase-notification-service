// Package config loads notification-service configuration from environment variables.
package config

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type Config struct {
	Env                                                                         string
	HTTPPort                                                                    string
	HTTPReadTimeout, HTTPWriteTimeout, HTTPIdleTimeout, HTTPShutdownTimeout     time.Duration
	LogLevel                                                                    string
	DatabaseURL                                                                 string
	DatabaseConnectTimeout                                                      time.Duration
	FirebaseProjectID                                                           string
	FirebaseServiceAccountJSON                                                  []byte
	AppleBundleID, AppleKeyID, AppleIssuerID, ApplePrivateKey, AppleEnvironment string
	AuthJWKSURL                                                                 string
	RedisAddr                                                                   string
	RedisConnectTimeout                                                         time.Duration
}

func Load() (*Config, error) {
	env := getEnv("APP_ENV", "development")
	appleEnv := getEnv("APPLE_ENVIRONMENT", "")
	if appleEnv == "" {
		appleEnv = "Sandbox"
		if env == "production" {
			appleEnv = "Production"
		}
	}

	cfg := &Config{
		Env: env, HTTPPort: getEnv("HTTP_PORT", "8080"), LogLevel: getEnv("LOG_LEVEL", "info"),
		HTTPReadTimeout: getDuration("HTTP_READ_TIMEOUT", 5*time.Second), HTTPWriteTimeout: getDuration("HTTP_WRITE_TIMEOUT", 10*time.Second),
		HTTPIdleTimeout: getDuration("HTTP_IDLE_TIMEOUT", 60*time.Second), HTTPShutdownTimeout: getDuration("HTTP_SHUTDOWN_TIMEOUT", 15*time.Second),
		DatabaseURL: getEnv("DATABASE_URL", ""), DatabaseConnectTimeout: getDuration("DATABASE_CONNECT_TIMEOUT", 10*time.Second),
		FirebaseProjectID: getEnv("FIREBASE_PROJECT_ID", ""),
		AppleBundleID:     getEnv("APPLE_BUNDLE_ID", "com.beebase.production"), AppleKeyID: getEnv("APPLE_KEY_ID", ""), AppleIssuerID: getEnv("APPLE_ISSUER_ID", ""), ApplePrivateKey: getEnv("APPLE_PRIVATE_KEY", ""), AppleEnvironment: appleEnv,
		AuthJWKSURL: getEnv("AUTH_JWKS_URL", ""), RedisAddr: getEnv("REDIS_ADDR", ""), RedisConnectTimeout: getDuration("REDIS_CONNECT_TIMEOUT", 5*time.Second),
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("config: DATABASE_URL is required")
	}
	if cfg.FirebaseProjectID == "" {
		return nil, fmt.Errorf("config: FIREBASE_PROJECT_ID is required")
	}
	encoded := getEnv("FIREBASE_SERVICE_ACCOUNT_JSON_BASE64", "")
	if encoded == "" {
		return nil, fmt.Errorf("config: FIREBASE_SERVICE_ACCOUNT_JSON_BASE64 is required")
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("config: decode Firebase service account: %w", err)
	}
	var credential struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(decoded, &credential); err != nil || credential.ProjectID == "" {
		return nil, fmt.Errorf("config: Firebase service account JSON is invalid")
	}
	if credential.ProjectID != cfg.FirebaseProjectID {
		return nil, fmt.Errorf("config: Firebase project ID does not match service account")
	}
	cfg.FirebaseServiceAccountJSON = decoded
	if cfg.AuthJWKSURL == "" {
		return nil, fmt.Errorf("config: AUTH_JWKS_URL is required")
	}
	if cfg.RedisAddr == "" {
		return nil, fmt.Errorf("config: REDIS_ADDR is required")
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
func getDuration(key string, fallback time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
