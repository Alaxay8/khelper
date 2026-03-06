package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/alaxay8/khelper/internal/config"
	"github.com/alaxay8/khelper/internal/kube"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	ExitCodeGeneral        = 1
	ExitCodeNotFound       = 2
	ExitCodeAmbiguous      = 3
	ExitCodeUsage          = 4
	ExitCodeUnavailable    = 5
	ExitCodeDoctorFindings = 6
)

type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *ExitError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func NewExitError(code int, msg string) error {
	return &ExitError{Code: code, Err: errors.New(msg)}
}

func WrapExitError(code int, err error, msg string, args ...any) error {
	if err == nil {
		return &ExitError{Code: code, Err: fmt.Errorf(msg, args...)}
	}
	if msg == "" {
		return &ExitError{Code: code, Err: err}
	}
	return &ExitError{Code: code, Err: fmt.Errorf(msg+": %w", append(args, err)...)}
}

var (
	cfgViper = viper.New()
	cfg      config.Settings
	rootCmd  = &cobra.Command{
		Use:           "khelper",
		Short:         "Ergonomic Kubernetes helper CLI that complements kubectl",
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if err := loadRuntimeConfig(); err != nil {
				return err
			}
			maybeAutoInstallCompletion(cmd)
			return nil
		},
	}
)

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		code := exitCode(err)
		if code != ExitCodeDoctorFindings {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		}
		os.Exit(code)
	}
}

func Config() config.Settings {
	return cfg
}

func debugf(format string, args ...any) {
	if !cfg.Verbose {
		return
	}
	fmt.Fprintf(os.Stderr, "DEBUG: "+format+"\n", args...)
}

func init() {
	cobra.OnInitialize()

	config.InitViper(cfgViper)
	rootCmd.CompletionOptions.DisableDefaultCmd = true

	flags := rootCmd.PersistentFlags()
	flags.String("kubeconfig", config.DefaultKubeconfigPath(), "Path to kubeconfig file")
	flags.String("context", "", "Kubeconfig context override")
	flags.StringP("namespace", "n", "", "Namespace override")
	flags.Bool("verbose", false, "Enable debug logging")
	flags.StringP("output", "o", "table", "Output format: table|json")

	_ = cfgViper.BindPFlag("kubeconfig", flags.Lookup("kubeconfig"))
	_ = cfgViper.BindPFlag("context", flags.Lookup("context"))
	_ = cfgViper.BindPFlag("namespace", flags.Lookup("namespace"))
	_ = cfgViper.BindPFlag("verbose", flags.Lookup("verbose"))
	_ = cfgViper.BindPFlag("output", flags.Lookup("output"))

	rootCmd.AddCommand(
		newVersionCmd(),
		newCompletionCmd(),
		newCompletionInstallCmd(),
		newContextCmd(),
		newNamespaceCmd(),
		newPodsCmd(),
		newLogsCmd(),
		newEventsCmd(),
		newRestartCmd(),
		newShellCmd(),
		newTopCmd(),
		newDoctorCmd(),
	)
}

func loadRuntimeConfig() error {
	if err := cfgViper.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return WrapExitError(ExitCodeGeneral, err, "read config file")
		}
	}

	loaded, err := config.Load(cfgViper)
	if err != nil {
		return WrapExitError(ExitCodeUsage, err, "invalid configuration")
	}
	cfg = loaded

	if cfg.Verbose {
		if used := cfgViper.ConfigFileUsed(); used != "" {
			debugf("using config file: %s", used)
		}
		debugf("kubeconfig=%s context=%s namespace=%s output=%s", cfg.Kubeconfig, cfg.Context, cfg.Namespace, cfg.Output)
	}

	return nil
}

func exitCode(err error) int {
	var ee *ExitError
	if errors.As(err, &ee) {
		return ee.Code
	}

	var amb *kube.AmbiguousMatchError
	if errors.As(err, &amb) {
		return ExitCodeAmbiguous
	}

	var nf *kube.NotFoundError
	if errors.As(err, &nf) {
		return ExitCodeNotFound
	}

	if errors.Is(err, kube.ErrMetricsUnavailable) {
		return ExitCodeUnavailable
	}

	return ExitCodeGeneral
}
