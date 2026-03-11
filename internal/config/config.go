package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

const (
	DefaultOutput         = "table"
	DefaultRequestTimeout = 30 * time.Second
)

// Settings contains application-level runtime configuration.
type Settings struct {
	Kubeconfig     string
	Context        string
	Namespace      string
	Output         string
	Verbose        bool
	RequestTimeout time.Duration
}

func DefaultKubeconfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".kube/config"
	}
	return filepath.Join(home, ".kube", "config")
}

func InitViper(v *viper.Viper) {
	v.SetConfigName(".khelper")
	v.SetConfigType("yaml")
	if home, err := os.UserHomeDir(); err == nil {
		v.AddConfigPath(home)
	}

	v.SetEnvPrefix("KHELPER")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv()

	v.SetDefault("kubeconfig", DefaultKubeconfigPath())
	v.SetDefault("output", DefaultOutput)
	v.SetDefault("request_timeout", DefaultRequestTimeout)
}

func Load(v *viper.Viper) (Settings, error) {
	requestTimeout, err := parseRequestTimeout(v)
	if err != nil {
		return Settings{}, err
	}

	cfg := Settings{
		Kubeconfig:     strings.TrimSpace(v.GetString("kubeconfig")),
		Context:        strings.TrimSpace(v.GetString("context")),
		Namespace:      strings.TrimSpace(v.GetString("namespace")),
		Output:         strings.ToLower(strings.TrimSpace(v.GetString("output"))),
		Verbose:        v.GetBool("verbose"),
		RequestTimeout: requestTimeout,
	}

	if cfg.Kubeconfig == "" {
		cfg.Kubeconfig = DefaultKubeconfigPath()
	}

	absPath, err := filepath.Abs(cfg.Kubeconfig)
	if err != nil {
		return Settings{}, fmt.Errorf("resolve kubeconfig path: %w", err)
	}
	cfg.Kubeconfig = absPath

	switch cfg.Output {
	case "", "table":
		cfg.Output = "table"
	case "json":
	default:
		return Settings{}, fmt.Errorf("invalid output format %q (allowed: table, json)", cfg.Output)
	}
	if cfg.RequestTimeout < 0 {
		return Settings{}, fmt.Errorf("request timeout must be >= 0")
	}

	return cfg, nil
}

func parseRequestTimeout(v *viper.Viper) (time.Duration, error) {
	raw := strings.TrimSpace(v.GetString("request_timeout"))
	if raw == "" {
		return v.GetDuration("request_timeout"), nil
	}

	timeout, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid request timeout %q: %w", raw, err)
	}
	return timeout, nil
}
