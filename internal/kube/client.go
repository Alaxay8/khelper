package kube

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alaxay8/khelper/internal/config"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"
)

// ClientBundle contains fully initialized Kubernetes clients and resolved runtime scope.
type ClientBundle struct {
	RESTConfig     *rest.Config
	Clientset      kubernetes.Interface
	RawConfig      clientcmdapi.Config
	Namespace      string
	CurrentContext string
}

func NewClientBundle(settings config.Settings) (*ClientBundle, error) {
	if settings.RequestTimeout < 0 {
		return nil, fmt.Errorf("request timeout must be >= 0")
	}

	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: settings.Kubeconfig}
	overrides := &clientcmd.ConfigOverrides{}
	if settings.Context != "" {
		overrides.CurrentContext = settings.Context
	}

	deferredCfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)

	restCfg, err := deferredCfg.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("build kubernetes REST config: %w", err)
	}
	restCfg.Timeout = normalizeRequestTimeout(settings.RequestTimeout)

	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes clientset: %w", err)
	}

	rawCfg, err := deferredCfg.RawConfig()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}

	ctxName := CurrentContextName(rawCfg, settings.Context)
	namespace := strings.TrimSpace(settings.Namespace)
	if namespace == "" {
		namespace, _, err = deferredCfg.Namespace()
		if err != nil {
			namespace = "default"
		}
	}
	if namespace == "" {
		namespace = "default"
	}

	return &ClientBundle{
		RESTConfig:     restCfg,
		Clientset:      clientset,
		RawConfig:      rawCfg,
		Namespace:      namespace,
		CurrentContext: ctxName,
	}, nil
}

func normalizeRequestTimeout(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return 0
	}
	return timeout
}

func NewMetricsClient(restCfg *rest.Config) (metricsclient.Interface, error) {
	client, err := metricsclient.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("create metrics client: %w", err)
	}
	return client, nil
}

func LoadRawKubeconfig(path string) (clientcmdapi.Config, error) {
	cfg, err := clientcmd.LoadFromFile(path)
	if err != nil {
		return clientcmdapi.Config{}, fmt.Errorf("load kubeconfig file %s: %w", path, err)
	}
	return *cfg, nil
}

func WriteRawKubeconfigAtomic(path string, cfg clientcmdapi.Config) error {
	data, err := clientcmd.Write(cfg)
	if err != nil {
		return fmt.Errorf("serialize kubeconfig: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("ensure kubeconfig directory %s: %w", dir, err)
	}

	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}

	tmpFile, err := os.CreateTemp(dir, ".khelper-kubeconfig-*")
	if err != nil {
		return fmt.Errorf("create temp kubeconfig file: %w", err)
	}
	tmpPath := tmpFile.Name()

	cleanup := func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
	}

	if _, err := tmpFile.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("write temp kubeconfig file: %w", err)
	}
	if err := tmpFile.Chmod(mode); err != nil {
		cleanup()
		return fmt.Errorf("set temp kubeconfig file mode: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("flush temp kubeconfig file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp kubeconfig file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace kubeconfig file: %w", err)
	}

	return nil
}

func CurrentContextName(rawCfg clientcmdapi.Config, override string) string {
	override = strings.TrimSpace(override)
	if override != "" {
		return override
	}
	return rawCfg.CurrentContext
}
