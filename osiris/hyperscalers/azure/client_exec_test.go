// client_exec_test.go - Tests for the Azure CLI execution helpers of
// the OSIRIS JSON Microsoft Azure producer: classification of az error
// output as transient (retried) or permanent (returned at once).

package azure

import "testing"

func TestIsTransientAZError(t *testing.T) {
	transient := []string{
		"ERROR: [SSL: UNEXPECTED_EOF_WHILE_READING] EOF occurred in violation of protocol (_ssl.c:1006)",
		"ERROR: ('Connection aborted.', RemoteDisconnected('Remote end closed connection without response'))",
		"ERROR: (TooManyRequests) Too many requests, retry later",
		"ERROR: (ServiceUnavailable) The service is temporarily unavailable",
		"ERROR: HTTPSConnectionPool(host='management.example.com', port=443): Read timed out. (read timeout=60)",
	}
	permanent := []string{
		"ERROR: (AuthorizationFailed) The client does not have authorization to perform action",
		"ERROR: AADSTS530003: Your device is required to be managed to access this resource.",
		"ERROR: (ResourceNotFound) The Resource 'LAB-VM01' was not found.",
		"ERROR: unrecognized arguments: --bogus",
		"",
	}
	for _, s := range transient {
		if !isTransientAZError(s) {
			t.Errorf("must be retried: %q", s)
		}
	}
	for _, s := range permanent {
		if isTransientAZError(s) {
			t.Errorf("must not be retried: %q", s)
		}
	}
}
