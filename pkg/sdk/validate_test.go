// validate_test.go - Tests for schema pattern validators (type formats, status enums,
// direction values, provider constraints).
//
// For an introduction to OSIRIS JSON Producer Development Guidelines see:
// "[OSIRIS-PRODUCER-GUIDELINES]."
//
// [OSIRIS-PRODUCER-GUIDELINES]: https://osirisjson.org/en/docs/producers/getting-started

package sdk

import (
	"testing"
)

func TestValidateResourceType(t *testing.T) {
	valid := []string{"compute.vm", "network.switch", "network.switch.leaf", "storage.volume.block", "application.database"}
	for _, v := range valid {
		if err := ValidateResourceType(v); err != nil {
			t.Errorf("ValidateResourceType(%q) unexpected error: %v", v, err)
		}
	}

	invalid := []string{"", "network", "UPPER.case", "has spaces.vm", "compute.", ".vm", "compute..vm"}
	for _, v := range invalid {
		if err := ValidateResourceType(v); err == nil {
			t.Errorf("ValidateResourceType(%q) expected error, got nil", v)
		}
	}
}

func TestValidateConnectionType(t *testing.T) {
	valid := []string{"network", "network.l2", "dataflow.tcp", "dependency", "contains", "attached"}
	for _, v := range valid {
		if err := ValidateConnectionType(v); err != nil {
			t.Errorf("ValidateConnectionType(%q) unexpected error: %v", v, err)
		}
	}

	invalid := []string{"", "UPPER", "has spaces", "network.", ".l2"}
	for _, v := range invalid {
		if err := ValidateConnectionType(v); err == nil {
			t.Errorf("ValidateConnectionType(%q) expected error, got nil", v)
		}
	}
}

func TestValidateGroupType(t *testing.T) {
	valid := []string{"physical.site", "logical.environment", "network.vpc", "physical"}
	for _, v := range valid {
		if err := ValidateGroupType(v); err != nil {
			t.Errorf("ValidateGroupType(%q) unexpected error: %v", v, err)
		}
	}
}

func TestValidateProviderName(t *testing.T) {
	valid := []string{"aws", "cisco", "custom", "arista", "nokia"}
	for _, v := range valid {
		if err := ValidateProviderName(v); err != nil {
			t.Errorf("ValidateProviderName(%q) unexpected error: %v", v, err)
		}
	}

	invalid := []string{"", "AWS", "has spaces", "cisco!", "my-provider"}
	for _, v := range invalid {
		if err := ValidateProviderName(v); err == nil {
			t.Errorf("ValidateProviderName(%q) expected error, got nil", v)
		}
	}
}

func TestValidateNamespace(t *testing.T) {
	valid := []string{"osiris.com.acme", "osiris.internal", "osiris.io.mycompany"}
	for _, v := range valid {
		if err := ValidateNamespace(v); err != nil {
			t.Errorf("ValidateNamespace(%q) unexpected error: %v", v, err)
		}
	}

	invalid := []string{"", "osiris.", "com.acme", "osiris.UPPER", "osiris.com.acme!"}
	for _, v := range invalid {
		if err := ValidateNamespace(v); err == nil {
			t.Errorf("ValidateNamespace(%q) expected error, got nil", v)
		}
	}
}

func TestValidateStatus(t *testing.T) {
	valid := []string{"", "active", "inactive", "degraded", "retired", "unknown"}
	for _, v := range valid {
		if err := ValidateStatus(v); err != nil {
			t.Errorf("ValidateStatus(%q) unexpected error: %v", v, err)
		}
	}

	invalid := []string{"running", "stopped", "Active", "ACTIVE"}
	for _, v := range invalid {
		if err := ValidateStatus(v); err == nil {
			t.Errorf("ValidateStatus(%q) expected error, got nil", v)
		}
	}
}

func TestValidateDirection(t *testing.T) {
	valid := []string{"", "bidirectional", "forward", "reverse"}
	for _, v := range valid {
		if err := ValidateDirection(v); err != nil {
			t.Errorf("ValidateDirection(%q) unexpected error: %v", v, err)
		}
	}

	invalid := []string{"inbound", "outbound", "Bidirectional"}
	for _, v := range invalid {
		if err := ValidateDirection(v); err == nil {
			t.Errorf("ValidateDirection(%q) expected error, got nil", v)
		}
	}
}

func TestValidateID(t *testing.T) {
	if err := ValidateID("valid-id", "test"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if err := ValidateID("", "test"); err == nil {
		t.Error("expected error for empty ID")
	}
	if err := ValidateID("   ", "test"); err == nil {
		t.Error("expected error for whitespace-only ID")
	}
}

func TestValidateProvider(t *testing.T) {
	// Valid standard provider.
	if err := ValidateProvider(Provider{Name: "cisco"}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Valid custom provider.
	if err := ValidateProvider(Provider{Name: "custom", Namespace: "osiris.com.acme"}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Custom without namespace.
	if err := ValidateProvider(Provider{Name: "custom"}); err == nil {
		t.Error("expected error: custom without namespace")
	}

	// Custom with invalid namespace.
	if err := ValidateProvider(Provider{Name: "custom", Namespace: "bad"}); err == nil {
		t.Error("expected error: custom with invalid namespace")
	}

	// Invalid name.
	if err := ValidateProvider(Provider{Name: "UPPER"}); err == nil {
		t.Error("expected error: uppercase provider name")
	}
}
