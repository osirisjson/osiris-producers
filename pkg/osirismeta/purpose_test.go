package osirismeta

import "testing"

func TestDefaultIsDocumentation(t *testing.T) {
	if got := Default(); got != PurposeDocumentation {
		t.Fatalf("Default() = %q, want %q", got, PurposeDocumentation)
	}
}

func TestParsePurpose(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    Purpose
		wantErr bool
	}{
		{"empty resolves to default", "", PurposeDocumentation, false},
		{"documentation", "documentation", PurposeDocumentation, false},
		{"audit", "audit", PurposeAudit, false},
		{"unknown rejected", "verbose", "", true},
		{"case sensitive", "Audit", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParsePurpose(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParsePurpose(%q) err = nil, want error", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParsePurpose(%q) err = %v, want nil", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("ParsePurpose(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestPurposeString(t *testing.T) {
	if PurposeAudit.String() != "audit" {
		t.Fatalf("PurposeAudit.String() = %q", PurposeAudit.String())
	}
}
