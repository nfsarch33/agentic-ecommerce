package mem0

import (
	"testing"
)

func TestConfigFromEnv_Defaults(t *testing.T) {
	t.Setenv("EC_MEM0_ENABLED", "")
	t.Setenv("EC_MEM0_TIMEOUT_SECONDS", "")
	t.Setenv("EC_MEM0_ENDPOINT", "")

	cfg := ConfigFromEnv()
	if !cfg.Enabled {
		t.Fatal("expected enabled=true by default")
	}
	if cfg.TimeoutSeconds != 5 {
		t.Fatalf("timeout = %d, want 5", cfg.TimeoutSeconds)
	}
	if cfg.Endpoint != "" {
		t.Fatalf("endpoint = %q, want empty", cfg.Endpoint)
	}
}

func TestConfigFromEnv_Disabled(t *testing.T) {
	t.Setenv("EC_MEM0_ENABLED", "false")
	t.Setenv("EC_MEM0_TIMEOUT_SECONDS", "")
	t.Setenv("EC_MEM0_ENDPOINT", "")

	cfg := ConfigFromEnv()
	if cfg.Enabled {
		t.Fatal("expected enabled=false")
	}
}

func TestConfigFromEnv_DisabledZero(t *testing.T) {
	t.Setenv("EC_MEM0_ENABLED", "0")
	t.Setenv("EC_MEM0_TIMEOUT_SECONDS", "")
	t.Setenv("EC_MEM0_ENDPOINT", "")

	cfg := ConfigFromEnv()
	if cfg.Enabled {
		t.Fatal("expected enabled=false for '0'")
	}
}

func TestConfigFromEnv_CustomTimeout(t *testing.T) {
	t.Setenv("EC_MEM0_ENABLED", "true")
	t.Setenv("EC_MEM0_TIMEOUT_SECONDS", "30")
	t.Setenv("EC_MEM0_ENDPOINT", "http://mem0:8080")

	cfg := ConfigFromEnv()
	if cfg.TimeoutSeconds != 30 {
		t.Fatalf("timeout = %d, want 30", cfg.TimeoutSeconds)
	}
	if cfg.Endpoint != "http://mem0:8080" {
		t.Fatalf("endpoint = %q", cfg.Endpoint)
	}
}

func TestConfigFromEnv_InvalidTimeout(t *testing.T) {
	t.Setenv("EC_MEM0_ENABLED", "true")
	t.Setenv("EC_MEM0_TIMEOUT_SECONDS", "not-a-number")
	t.Setenv("EC_MEM0_ENDPOINT", "")

	cfg := ConfigFromEnv()
	if cfg.TimeoutSeconds != 5 {
		t.Fatalf("timeout = %d, want default 5 for invalid input", cfg.TimeoutSeconds)
	}
}

func TestConfigFromEnv_NegativeTimeout(t *testing.T) {
	t.Setenv("EC_MEM0_ENABLED", "true")
	t.Setenv("EC_MEM0_TIMEOUT_SECONDS", "-1")
	t.Setenv("EC_MEM0_ENDPOINT", "")

	cfg := ConfigFromEnv()
	if cfg.TimeoutSeconds != 5 {
		t.Fatalf("timeout = %d, want default 5 for negative input", cfg.TimeoutSeconds)
	}
}

func TestNewClient_NilLogger(t *testing.T) {
	client := NewClient(nil, Config{Enabled: false}, nil, nil)
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}
