package config

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestLoadDefaults(t *testing.T) {
	t.Parallel()

	v := viper.New()
	InitViper(v)

	cfg, err := Load(v)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Output != DefaultOutput {
		t.Fatalf("expected default output %q, got %q", DefaultOutput, cfg.Output)
	}
	if cfg.Kubeconfig == "" {
		t.Fatal("expected kubeconfig path to be set")
	}
	if !filepath.IsAbs(cfg.Kubeconfig) {
		t.Fatalf("expected absolute kubeconfig path, got %q", cfg.Kubeconfig)
	}
}

func TestLoadNormalizesFields(t *testing.T) {
	t.Parallel()

	v := viper.New()
	v.Set("kubeconfig", " ./tmp/kubeconfig ")
	v.Set("context", "  dev ")
	v.Set("namespace", " shop  ")
	v.Set("output", " JSON ")
	v.Set("verbose", true)

	cfg, err := Load(v)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	wantPath, err := filepath.Abs("./tmp/kubeconfig")
	if err != nil {
		t.Fatalf("failed to build expected absolute path: %v", err)
	}

	if cfg.Kubeconfig != wantPath {
		t.Fatalf("unexpected kubeconfig path: got %q, want %q", cfg.Kubeconfig, wantPath)
	}
	if cfg.Context != "dev" {
		t.Fatalf("unexpected context: %q", cfg.Context)
	}
	if cfg.Namespace != "shop" {
		t.Fatalf("unexpected namespace: %q", cfg.Namespace)
	}
	if cfg.Output != "json" {
		t.Fatalf("unexpected output: %q", cfg.Output)
	}
	if !cfg.Verbose {
		t.Fatal("expected verbose=true")
	}
}

func TestLoadRejectsInvalidOutput(t *testing.T) {
	t.Parallel()

	v := viper.New()
	v.Set("output", "yaml")

	_, err := Load(v)
	if err == nil {
		t.Fatal("expected error for invalid output")
	}
	if !strings.Contains(err.Error(), "invalid output format") {
		t.Fatalf("unexpected error: %v", err)
	}
}
