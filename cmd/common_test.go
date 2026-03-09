package cmd

import (
	"errors"
	"testing"
	"time"
)

func TestParseNonNegativeDurationFlag(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		raw          string
		defaultValue time.Duration
		want         time.Duration
		wantErr      bool
	}{
		{name: "default", raw: "", defaultValue: time.Hour, want: time.Hour},
		{name: "valid", raw: "30m", defaultValue: time.Hour, want: 30 * time.Minute},
		{name: "valid with spaces", raw: " 2h ", defaultValue: time.Hour, want: 2 * time.Hour},
		{name: "negative", raw: "-1m", defaultValue: time.Hour, wantErr: true},
		{name: "invalid", raw: "soon", defaultValue: time.Hour, wantErr: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseNonNegativeDurationFlag("since", tc.raw, tc.defaultValue)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				var ee *ExitError
				if !errors.As(err, &ee) {
					t.Fatalf("expected ExitError, got %T", err)
				}
				if ee.Code != ExitCodeUsage {
					t.Fatalf("expected usage exit code, got %d", ee.Code)
				}
				return
			}

			if err != nil {
				t.Fatalf("parseNonNegativeDurationFlag returned error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("unexpected duration: got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestResolveNamespaceScope(t *testing.T) {
	t.Parallel()

	if got := resolveNamespaceScope("shop", false); got != "shop" {
		t.Fatalf("expected namespace shop, got %q", got)
	}
	if got := resolveNamespaceScope("shop", true); got != "*" {
		t.Fatalf("expected all namespaces marker '*', got %q", got)
	}
}
