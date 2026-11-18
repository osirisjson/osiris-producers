// repository_test.go - TestRepositoryDataPolicy runs the test data
// policy over every tracked file of the repository and fails on any
// unsuppressed finding or allowlist problem.
//
// Run it on its own with:
//
//	go test ./internal/policies/testdatapolicy -run TestRepositoryDataPolicy -count=1 -v
//
// Set TDP_INCLUDE_UNTRACKED=1 to also scan untracked files that are
// not ignored, for feedback before staging new files.

package testdatapolicy

import (
	"os"
	"testing"
	"time"
)

func TestRepositoryDataPolicy(t *testing.T) {
	ctx := t.Context()
	src, err := OpenRepository(ctx, ".")
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	defer src.Close()
	src.IncludeUntracked = os.Getenv("TDP_INCLUDE_UNTRACKED") == "1"

	rep, err := Scan(ctx, src, Options{Today: time.Now().UTC()})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, f := range rep.Violations {
		t.Error(f)
	}
	for _, f := range rep.ExceptionProblems {
		t.Error(f)
	}
	t.Log("\n" + rep.Summary())
}
