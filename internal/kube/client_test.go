package kube

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alaxay8/khelper/internal/config"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestCurrentContextName(t *testing.T) {
	t.Parallel()

	raw := clientcmdapi.Config{CurrentContext: "dev"}

	if got := CurrentContextName(raw, ""); got != "dev" {
		t.Fatalf("expected current context dev, got %q", got)
	}
	if got := CurrentContextName(raw, " prod "); got != "prod" {
		t.Fatalf("expected override context prod, got %q", got)
	}
}

func TestWriteAndLoadRawKubeconfigAtomic(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "kube", "config")
	cfg := testRawKubeconfig()

	if err := WriteRawKubeconfigAtomic(path, cfg); err != nil {
		t.Fatalf("WriteRawKubeconfigAtomic returned error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat kubeconfig: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected kubeconfig mode 0600, got %o", got)
	}

	loaded, err := LoadRawKubeconfig(path)
	if err != nil {
		t.Fatalf("LoadRawKubeconfig returned error: %v", err)
	}
	if loaded.CurrentContext != cfg.CurrentContext {
		t.Fatalf("unexpected current context: got %q, want %q", loaded.CurrentContext, cfg.CurrentContext)
	}

	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatalf("failed to chmod kubeconfig: %v", err)
	}
	cfg.CurrentContext = "prod"
	if err := WriteRawKubeconfigAtomic(path, cfg); err != nil {
		t.Fatalf("second WriteRawKubeconfigAtomic returned error: %v", err)
	}

	info, err = os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat kubeconfig after rewrite: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("expected kubeconfig mode 0640 after rewrite, got %o", got)
	}
}

func TestNewClientBundle(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config")
	if err := WriteRawKubeconfigAtomic(path, testRawKubeconfig()); err != nil {
		t.Fatalf("failed to write kubeconfig: %v", err)
	}

	bundle, err := NewClientBundle(config.Settings{
		Kubeconfig:     path,
		RequestTimeout: 45 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewClientBundle returned error: %v", err)
	}

	if bundle.Clientset == nil {
		t.Fatal("expected non-nil kubernetes clientset")
	}
	if bundle.RESTConfig == nil {
		t.Fatal("expected non-nil REST config")
	}
	if bundle.CurrentContext != "dev" {
		t.Fatalf("expected context dev, got %q", bundle.CurrentContext)
	}
	if bundle.Namespace != "shop" {
		t.Fatalf("expected namespace shop, got %q", bundle.Namespace)
	}
	if bundle.RESTConfig.Timeout != 45*time.Second {
		t.Fatalf("expected REST timeout 45s, got %s", bundle.RESTConfig.Timeout)
	}
}

func TestNewClientBundleWithContextOverride(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config")
	if err := WriteRawKubeconfigAtomic(path, testRawKubeconfig()); err != nil {
		t.Fatalf("failed to write kubeconfig: %v", err)
	}

	bundle, err := NewClientBundle(config.Settings{
		Kubeconfig: path,
		Context:    "prod",
	})
	if err != nil {
		t.Fatalf("NewClientBundle returned error: %v", err)
	}

	if bundle.CurrentContext != "prod" {
		t.Fatalf("expected context override prod, got %q", bundle.CurrentContext)
	}
	if bundle.Namespace != "ops" {
		t.Fatalf("expected namespace from prod context ops, got %q", bundle.Namespace)
	}
	if bundle.RESTConfig.Timeout != 0 {
		t.Fatalf("expected REST timeout unset by default, got %s", bundle.RESTConfig.Timeout)
	}
}

func TestNewClientBundleRejectsNegativeTimeout(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config")
	if err := WriteRawKubeconfigAtomic(path, testRawKubeconfig()); err != nil {
		t.Fatalf("failed to write kubeconfig: %v", err)
	}

	_, err := NewClientBundle(config.Settings{
		Kubeconfig:     path,
		RequestTimeout: -1 * time.Second,
	})
	if err == nil {
		t.Fatal("expected error for negative request timeout")
	}
}

func testRawKubeconfig() clientcmdapi.Config {
	return clientcmdapi.Config{
		CurrentContext: "dev",
		Clusters: map[string]*clientcmdapi.Cluster{
			"cluster-a": {
				Server:                "https://127.0.0.1:6443",
				InsecureSkipTLSVerify: true,
			},
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"user-a": {Token: "token-a"},
		},
		Contexts: map[string]*clientcmdapi.Context{
			"dev": {
				Cluster:   "cluster-a",
				AuthInfo:  "user-a",
				Namespace: "shop",
			},
			"prod": {
				Cluster:   "cluster-a",
				AuthInfo:  "user-a",
				Namespace: "ops",
			},
		},
	}
}
