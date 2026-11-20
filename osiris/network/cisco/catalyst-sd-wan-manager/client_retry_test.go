// client_retry_test.go - Tests for the bounded retry and session
// renewal of the Cisco Catalyst SD-WAN Manager REST API client: one
// retry for a timeout or HTTP 5xx, none for a denial and one
// re-login per run when the session expires (HTTP 401, or the login
// page re-served with HTTP 200), shared by concurrent requests.
//
// OSIRIS JSON Producer for Cisco Catalyst SD-WAN Manager introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification
package catalystsdwanmanager

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// sessionServer is a Manager whose /dataservice/device endpoint is
// scripted per call; logins and token requests are counted.
type sessionServer struct {
	logins, tokens, devices atomic.Int32
	device                  func(call int32, w http.ResponseWriter, r *http.Request)
}

func (s *sessionServer) handler(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/j_security_check":
		s.logins.Add(1)
	case "/dataservice/client/token":
		// The first login gets "test", every renewal "changeme".
		if s.tokens.Add(1) == 1 {
			fmt.Fprint(w, `{"token":"test"}`)
		} else {
			fmt.Fprint(w, `{"token":"changeme"}`)
		}
	case "/dataservice/device":
		s.device(s.devices.Add(1), w, r)
	}
}

const oneDevice = `{"data":[{"deviceId":"192.0.2.1","host-name":"LAB-RTR01"}]}`

func loggedInClient(t *testing.T, s *sessionServer) (*Client, func()) {
	t.Helper()
	ts := httptest.NewTLSServer(http.HandlerFunc(s.handler))
	c := newSecureTestClient(t, ts)
	if err := c.Login("admin", "changeme"); err != nil {
		ts.Close()
		t.Fatalf("Login: %v", err)
	}
	return c, ts.Close
}

func TestFetch_RetriesTimeoutOnce(t *testing.T) {
	s := &sessionServer{device: func(call int32, w http.ResponseWriter, r *http.Request) {
		if call == 1 {
			time.Sleep(300 * time.Millisecond)
		}
		fmt.Fprint(w, oneDevice)
	}}
	c, stop := loggedInClient(t, s)
	defer stop()
	c.httpClient.Timeout = 100 * time.Millisecond
	if _, err := c.GetDevices(); err != nil {
		t.Fatalf("GetDevices after one timeout: %v", err)
	}
	if got := s.devices.Load(); got != 2 {
		t.Errorf("device calls = %d, want 2 (one retry)", got)
	}
}

func TestFetch_RetryIsBounded(t *testing.T) {
	for _, tt := range []struct {
		name     string
		status   int
		calls    int32
		category string
	}{
		{"5xx retried once", http.StatusServiceUnavailable, 2, categoryServerError},
		{"403 never retried", http.StatusForbidden, 1, categoryDenied},
		{"404 never retried", http.StatusNotFound, 1, categoryNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := &sessionServer{device: func(_ int32, w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tt.status) }}
			c, stop := loggedInClient(t, s)
			defer stop()
			_, err := c.GetDevices()
			if failureCategory(err) != tt.category {
				t.Errorf("category = %q, want %q", failureCategory(err), tt.category)
			}
			if got := s.devices.Load(); got != tt.calls {
				t.Errorf("device calls = %d, want %d", got, tt.calls)
			}
		})
	}
}

func TestFetch_RenewsExpiredSessionOnce(t *testing.T) {
	for _, tt := range []struct {
		name    string
		expired func(w http.ResponseWriter)
	}{
		{"HTTP 401", func(w http.ResponseWriter) { w.WriteHeader(http.StatusUnauthorized) }},
		{"login page with 200", func(w http.ResponseWriter) { fmt.Fprint(w, "<!DOCTYPE html><html><body>login</body></html>") }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var sawToken atomic.Value
			s := &sessionServer{device: func(call int32, w http.ResponseWriter, r *http.Request) {
				if call == 1 {
					tt.expired(w)
					return
				}
				sawToken.Store(r.Header.Get("X-XSRF-TOKEN"))
				fmt.Fprint(w, oneDevice)
			}}
			c, stop := loggedInClient(t, s)
			defer stop()
			if _, err := c.GetDevices(); err != nil {
				t.Fatalf("GetDevices after expiry: %v", err)
			}
			if s.logins.Load() != 2 {
				t.Errorf("logins = %d, want 2", s.logins.Load())
			}
			if sawToken.Load() != "changeme" {
				t.Errorf("retried request sent token %v, want the renewed one", sawToken.Load())
			}
		})
	}
}

func TestFetch_AtMostOneRenewalPerRun(t *testing.T) {
	s := &sessionServer{device: func(_ int32, w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) }}
	c, stop := loggedInClient(t, s)
	defer stop()
	for i := 0; i < 3; i++ {
		_, err := c.GetDevices()
		if !errors.Is(err, errSessionExpired) || failureCategory(err) != categoryDenied {
			t.Fatalf("call %d: err = %v, want a denied session-expired error", i, err)
		}
	}
	if got := s.logins.Load(); got != 1+maxReauthentications {
		t.Errorf("logins = %d, want %d", got, 1+maxReauthentications)
	}
}

func TestFetch_ConcurrentExpiryRenewsOnce(t *testing.T) {
	var gate sync.Once
	ready := make(chan struct{})
	s := &sessionServer{}
	s.device = func(_ int32, w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-XSRF-TOKEN") == "test" {
			gate.Do(func() { close(ready) })
			<-ready
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, oneDevice)
	}
	c, stop := loggedInClient(t, s)
	defer stop()

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.GetDevices()
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent request failed: %v", err)
		}
	}
	if got := s.logins.Load(); got != 2 {
		t.Errorf("logins = %d, want 2 (one shared renewal)", got)
	}
}
