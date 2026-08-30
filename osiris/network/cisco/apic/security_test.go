// security_test.go - Credential-safety canary tests for the Cisco
// ACI/APIC producer. Proves that a login secret never
// reaches the emitted OSIRIS JSON document or the diagnostic log
// stream, in either --purpose mode.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
	"go.osirisjson.org/producers/pkg/sdk"
	"go.osirisjson.org/producers/pkg/testharness"
)

func TestCollect_NoCredentialLeak(t *testing.T) {
	const (
		canaryUser = "CANARY-USER-1a2b3c"
		canaryPass = "CANARY-PW-9f3a2b4c5d"
	)

	for _, purpose := range []string{"documentation", "audit"} {
		t.Run(purpose, func(t *testing.T) {
			ts := fixtureServer(t)
			defer ts.Close()

			var logBuf bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

			producer := &Producer{
				target: run.TargetConfig{Host: "test", Username: canaryUser, Password: canaryPass},
				cfg:    &Config{Purpose: purpose},
				ctx:    context.Background(),
				client: &Client{
					baseURL:    ts.URL,
					httpClient: ts.Client(),
					token:      "test-token",
					username:   canaryUser,
					logger:     logger,
					ctx:        context.Background(),
				},
			}
			ctx := testharness.NewTestContext(t, testharness.WithConfig(&sdk.ProducerConfig{
				Purpose:         purpose,
				SafeFailureMode: sdk.FailClosed,
			}))
			ctx.Logger = logger

			doc, err := producer.Collect(ctx)
			if err != nil {
				t.Fatalf("Collect failed: %v", err)
			}

			data, err := sdk.MarshalDocument(doc)
			if err != nil {
				t.Fatalf("MarshalDocument failed: %v", err)
			}

			if bytes.Contains(data, []byte(canaryPass)) {
				t.Error("password canary leaked into the emitted document")
			}
			if bytes.Contains(data, []byte(canaryUser)) {
				t.Error("username canary leaked into the emitted document")
			}
			if strings.Contains(logBuf.String(), canaryPass) {
				t.Error("password canary leaked into the log stream")
			}
		})
	}
}
