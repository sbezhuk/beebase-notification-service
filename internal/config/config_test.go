package config

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

func TestLoadDecodesFirebaseCredentialWithoutExposingIt(t *testing.T) {
	credential := `{"type":"service_account","project_id":"beebase-production","private_key":"PRIVATE"}`
	keys := []string{"DATABASE_URL", "FIREBASE_PROJECT_ID", "FIREBASE_SERVICE_ACCOUNT_JSON_BASE64", "AUTH_JWKS_URL", "REDIS_ADDR", "INTERNAL_SERVICE_TOKEN"}
	for _, key := range keys {
		t.Setenv(key, "")
	}
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("FIREBASE_PROJECT_ID", "beebase-production")
	t.Setenv("FIREBASE_SERVICE_ACCOUNT_JSON_BASE64", base64.StdEncoding.EncodeToString([]byte(credential)))
	t.Setenv("AUTH_JWKS_URL", "http://auth")
	t.Setenv("REDIS_ADDR", "redis:6379")
	t.Setenv("INTERNAL_SERVICE_TOKEN", "test-token")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if string(cfg.FirebaseServiceAccountJSON) != credential {
		t.Fatal("decoded credential differs from source")
	}
	if strings.Contains(os.Getenv("FIREBASE_SERVICE_ACCOUNT_JSON_BASE64"), "PRIVATE") {
		t.Fatal("test fixture unexpectedly exposes private key in encoded env")
	}
}

func TestLoadRejectsProjectMismatch(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("FIREBASE_PROJECT_ID", "beebase-production")
	t.Setenv("FIREBASE_SERVICE_ACCOUNT_JSON_BASE64", base64.StdEncoding.EncodeToString([]byte(`{"project_id":"other"}`)))
	t.Setenv("AUTH_JWKS_URL", "http://auth")
	t.Setenv("REDIS_ADDR", "redis:6379")
	t.Setenv("INTERNAL_SERVICE_TOKEN", "test-token")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("Load() error = %v, want project mismatch", err)
	}
}
