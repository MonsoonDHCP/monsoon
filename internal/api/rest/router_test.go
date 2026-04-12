package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monsoondhcp/monsoon/internal/audit"
	"github.com/monsoondhcp/monsoon/internal/auth"
	"github.com/monsoondhcp/monsoon/internal/discovery"
	"github.com/monsoondhcp/monsoon/internal/ipam"
	"github.com/monsoondhcp/monsoon/internal/lease"
	"github.com/monsoondhcp/monsoon/internal/metrics"
	"github.com/monsoondhcp/monsoon/internal/storage"
)

type fakeLeaseStore struct {
	items map[string]lease.Lease
}

func (f *fakeLeaseStore) Upsert(_ context.Context, l lease.Lease) error {
	if f.items == nil {
		f.items = map[string]lease.Lease{}
	}
	f.items[l.IP] = l
	return nil
}
func (f *fakeLeaseStore) GetByIP(_ context.Context, ip string) (lease.Lease, error) {
	l, ok := f.items[ip]
	if !ok {
		return lease.Lease{}, context.Canceled
	}
	return l, nil
}
func (f *fakeLeaseStore) GetByMAC(_ context.Context, _ string) ([]lease.Lease, error) {
	return nil, nil
}
func (f *fakeLeaseStore) GetByClientID(_ context.Context, _ []byte) ([]lease.Lease, error) {
	return nil, nil
}
func (f *fakeLeaseStore) ListBySubnet(_ context.Context, _ string) ([]lease.Lease, error) {
	return nil, nil
}
func (f *fakeLeaseStore) ListExpiringBefore(_ context.Context, _ time.Time) ([]lease.Lease, error) {
	return nil, nil
}
func (f *fakeLeaseStore) Delete(_ context.Context, ip string) error {
	delete(f.items, ip)
	return nil
}
func (f *fakeLeaseStore) ListAll(_ context.Context) ([]lease.Lease, error) {
	out := make([]lease.Lease, 0, len(f.items))
	for _, l := range f.items {
		out = append(out, l)
	}
	return out, nil
}

func TestLeaseListRoute(t *testing.T) {
	store := &fakeLeaseStore{items: map[string]lease.Lease{
		"10.0.1.10": {IP: "10.0.1.10", State: lease.StateBound},
	}}
	mux := http.NewServeMux()
	if err := RegisterRoutes(mux, RouterDeps{LeaseStore: store, Version: "test"}); err != nil {
		t.Fatalf("register routes failed: %v", err)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/leases", nil)
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status mismatch: %d", rr.Code)
	}
	var env Envelope
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if env.Data == nil {
		t.Fatalf("expected data")
	}
}

func TestLeaseGetAndReleaseRoutes(t *testing.T) {
	store := &fakeLeaseStore{items: map[string]lease.Lease{
		"10.0.1.10": {IP: "10.0.1.10", State: lease.StateBound},
	}}
	mux := http.NewServeMux()
	if err := RegisterRoutes(mux, RouterDeps{LeaseStore: store, Version: "test"}); err != nil {
		t.Fatalf("register routes failed: %v", err)
	}

	getRR := httptest.NewRecorder()
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/leases/10.0.1.10", nil)
	mux.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("get status mismatch: %d", getRR.Code)
	}

	relRR := httptest.NewRecorder()
	relReq := httptest.NewRequest(http.MethodPost, "/api/v1/leases/10.0.1.10/release", nil)
	mux.ServeHTTP(relRR, relReq)
	if relRR.Code != http.StatusOK {
		t.Fatalf("release status mismatch: %d", relRR.Code)
	}
	if store.items["10.0.1.10"].State != lease.StateReleased {
		t.Fatalf("lease not released")
	}
}

func TestDashboardStaticFallback(t *testing.T) {
	dist := t.TempDir()
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("<html><body>ok</body></html>"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	mux := http.NewServeMux()
	if err := RegisterRoutes(mux, RouterDeps{
		Version: "test",
		Dashboard: DashboardConfig{
			Enabled: true,
			DistDir: dist,
		},
	}); err != nil {
		t.Fatalf("register routes failed: %v", err)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/subnets", nil)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status mismatch: got %d want %d", rr.Code, http.StatusOK)
	}
	if rr.Body.Len() == 0 {
		t.Fatalf("expected html body")
	}
}

func TestDashboardEmbeddedFallback(t *testing.T) {
	mux := http.NewServeMux()
	if err := RegisterRoutes(mux, RouterDeps{
		Version: "test",
		Dashboard: DashboardConfig{
			Enabled: true,
			DistDir: filepath.Join(t.TempDir(), "missing-dist"),
		},
	}); err != nil {
		t.Fatalf("register routes failed: %v", err)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status mismatch: got %d want %d", rr.Code, http.StatusOK)
	}
	if rr.Body.Len() == 0 {
		t.Fatalf("expected embedded html body")
	}
}

func TestReservationAndAddressRoutes(t *testing.T) {
	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{"subnets", "reservations", "leases"})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer eng.Close()

	leaseStore := lease.NewStore(eng)
	engine := ipam.NewEngine(eng, leaseStore)
	if _, err := engine.UpsertSubnet(context.Background(), ipam.UpsertSubnetInput{
		CIDR:       "10.1.0.0/24",
		Name:       "Test",
		VLAN:       10,
		Gateway:    "10.1.0.1",
		DHCPEnable: true,
		PoolStart:  "10.1.0.10",
		PoolEnd:    "10.1.0.50",
		LeaseSec:   3600,
	}); err != nil {
		t.Fatalf("upsert subnet: %v", err)
	}
	if err := leaseStore.Upsert(context.Background(), lease.Lease{
		IP:       "10.1.0.11",
		MAC:      "AA:BB:CC:DD:EE:11",
		Hostname: "lease-host",
		State:    lease.StateBound,
		SubnetID: "10.1.0.0/24",
	}); err != nil {
		t.Fatalf("upsert lease: %v", err)
	}

	mux := http.NewServeMux()
	if err := RegisterRoutes(mux, RouterDeps{
		LeaseStore: leaseStore,
		IPAMEngine: engine,
		Version:    "test",
	}); err != nil {
		t.Fatalf("register routes failed: %v", err)
	}

	body := []byte(`{"mac":"AA:BB:CC:DD:EE:10","ip":"10.1.0.10","hostname":"reserved-host","subnet_cidr":"10.1.0.0/24"}`)
	saveRR := httptest.NewRecorder()
	saveReq := httptest.NewRequest(http.MethodPost, "/api/v1/reservations", bytes.NewReader(body))
	mux.ServeHTTP(saveRR, saveReq)
	if saveRR.Code != http.StatusOK {
		t.Fatalf("reservation upsert status: %d body=%s", saveRR.Code, saveRR.Body.String())
	}

	listRR := httptest.NewRecorder()
	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/reservations", nil)
	mux.ServeHTTP(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("reservation list status: %d body=%s", listRR.Code, listRR.Body.String())
	}

	addressRR := httptest.NewRecorder()
	addressReq := httptest.NewRequest(http.MethodGet, "/api/v1/addresses?subnet=10.1.0.0/24", nil)
	mux.ServeHTTP(addressRR, addressReq)
	if addressRR.Code != http.StatusOK {
		t.Fatalf("address list status: %d body=%s", addressRR.Code, addressRR.Body.String())
	}
	var env Envelope
	if err := json.Unmarshal(addressRR.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode address response: %v", err)
	}
	if env.Data == nil {
		t.Fatalf("expected address data")
	}
}

func TestAuthLoginAndMeRoutes(t *testing.T) {
	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{"users", "api_tokens", "api_tokens_by_hash"})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer eng.Close()

	authService := auth.NewService(eng, auth.ServiceOptions{CookieName: "monsoon_session", SessionDuration: time.Hour})
	if err := authService.BootstrapAdmin(context.Background(), "admin", "admin"); err != nil {
		t.Fatalf("bootstrap admin: %v", err)
	}

	mux := http.NewServeMux()
	if err := RegisterRoutes(mux, RouterDeps{
		AuthService:      authService,
		AuthSecureCookie: false,
		Version:          "test",
	}); err != nil {
		t.Fatalf("register routes failed: %v", err)
	}
	handler := Chain(mux, AuthMiddlewareFunc(authService, func() bool { return true }))

	loginRR := httptest.NewRecorder()
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader([]byte(`{"username":"admin","password":"admin"}`)))
	handler.ServeHTTP(loginRR, loginReq)
	if loginRR.Code != http.StatusOK {
		t.Fatalf("login status mismatch: got %d body=%s", loginRR.Code, loginRR.Body.String())
	}
	if got := loginRR.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected login to disable caching, got %q", got)
	}
	cookies := loginRR.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("expected login cookie")
	}

	meReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	meReq.AddCookie(cookies[0])
	meRR := httptest.NewRecorder()
	handler.ServeHTTP(meRR, meReq)
	if meRR.Code != http.StatusOK {
		t.Fatalf("me status mismatch: got %d body=%s", meRR.Code, meRR.Body.String())
	}
	if got := meRR.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected me endpoint to disable caching, got %q", got)
	}
}

func TestAuthBootstrapRouteOnlyWorksOnce(t *testing.T) {
	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{"users", "api_tokens", "api_tokens_by_hash"})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer eng.Close()

	authService := auth.NewService(eng, auth.ServiceOptions{CookieName: "monsoon_session", SessionDuration: time.Hour})

	mux := http.NewServeMux()
	if err := RegisterRoutes(mux, RouterDeps{
		AuthService:      authService,
		AuthSecureCookie: true,
		Version:          "test",
	}); err != nil {
		t.Fatalf("register routes failed: %v", err)
	}
	handler := Chain(mux, AuthMiddlewareFunc(authService, func() bool { return true }))

	firstRR := httptest.NewRecorder()
	firstReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", bytes.NewReader([]byte(`{"username":"admin","password":"secret-pass"}`)))
	handler.ServeHTTP(firstRR, firstReq)
	if firstRR.Code != http.StatusOK {
		t.Fatalf("first bootstrap failed: %d body=%s", firstRR.Code, firstRR.Body.String())
	}

	secondRR := httptest.NewRecorder()
	secondReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", bytes.NewReader([]byte(`{"username":"admin2","password":"another-pass"}`)))
	handler.ServeHTTP(secondRR, secondReq)
	if secondRR.Code != http.StatusConflict {
		t.Fatalf("expected bootstrap conflict, got %d body=%s", secondRR.Code, secondRR.Body.String())
	}
}

func TestPasswordChangeRotatesSessionAndRevokesPreviousCookie(t *testing.T) {
	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{"users", "api_tokens", "api_tokens_by_hash"})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer eng.Close()

	authService := auth.NewService(eng, auth.ServiceOptions{CookieName: "monsoon_session", SessionDuration: time.Hour})
	if err := authService.BootstrapAdmin(context.Background(), "admin", "admin"); err != nil {
		t.Fatalf("bootstrap admin: %v", err)
	}

	mux := http.NewServeMux()
	if err := RegisterRoutes(mux, RouterDeps{
		AuthService:      authService,
		AuthSecureCookie: false,
		Version:          "test",
	}); err != nil {
		t.Fatalf("register routes failed: %v", err)
	}
	handler := Chain(mux, AuthMiddlewareFunc(authService, func() bool { return true }))

	loginRR := httptest.NewRecorder()
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader([]byte(`{"username":"admin","password":"admin"}`)))
	handler.ServeHTTP(loginRR, loginReq)
	if loginRR.Code != http.StatusOK {
		t.Fatalf("login failed: %d body=%s", loginRR.Code, loginRR.Body.String())
	}
	initialCookies := loginRR.Result().Cookies()
	if len(initialCookies) == 0 {
		t.Fatalf("expected initial session cookie")
	}

	changeRR := httptest.NewRecorder()
	changeReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password", bytes.NewReader([]byte(`{"current_password":"admin","new_password":"new-admin-pass"}`)))
	changeReq.AddCookie(initialCookies[0])
	handler.ServeHTTP(changeRR, changeReq)
	if changeRR.Code != http.StatusOK {
		t.Fatalf("password change failed: %d body=%s", changeRR.Code, changeRR.Body.String())
	}
	rotatedCookies := changeRR.Result().Cookies()
	if len(rotatedCookies) == 0 {
		t.Fatalf("expected rotated session cookie")
	}
	if rotatedCookies[0].Value == initialCookies[0].Value {
		t.Fatalf("expected session cookie value to rotate")
	}

	oldMeRR := httptest.NewRecorder()
	oldMeReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	oldMeReq.AddCookie(initialCookies[0])
	handler.ServeHTTP(oldMeRR, oldMeReq)
	if oldMeRR.Code != http.StatusUnauthorized {
		t.Fatalf("expected previous session to be revoked, got %d", oldMeRR.Code)
	}

	newMeRR := httptest.NewRecorder()
	newMeReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	newMeReq.AddCookie(rotatedCookies[0])
	handler.ServeHTTP(newMeRR, newMeReq)
	if newMeRR.Code != http.StatusOK {
		t.Fatalf("expected rotated session to be valid, got %d body=%s", newMeRR.Code, newMeRR.Body.String())
	}

	oldLoginRR := httptest.NewRecorder()
	oldLoginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader([]byte(`{"username":"admin","password":"admin"}`)))
	handler.ServeHTTP(oldLoginRR, oldLoginReq)
	if oldLoginRR.Code != http.StatusUnauthorized {
		t.Fatalf("expected old password login to fail, got %d", oldLoginRR.Code)
	}

	newLoginRR := httptest.NewRecorder()
	newLoginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader([]byte(`{"username":"admin","password":"new-admin-pass"}`)))
	handler.ServeHTTP(newLoginRR, newLoginReq)
	if newLoginRR.Code != http.StatusOK {
		t.Fatalf("expected new password login to succeed, got %d body=%s", newLoginRR.Code, newLoginRR.Body.String())
	}
}

func TestAuthFailureMetricsAreExported(t *testing.T) {
	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{"users", "api_tokens", "api_tokens_by_hash", "audit"})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer eng.Close()

	authService := auth.NewService(eng, auth.ServiceOptions{CookieName: "monsoon_session", SessionDuration: time.Hour})
	if err := authService.BootstrapAdmin(context.Background(), "admin", "admin"); err != nil {
		t.Fatalf("bootstrap admin: %v", err)
	}
	reg := metrics.NewRegistry()
	auditLogger := audit.NewLogger(eng)

	mux := http.NewServeMux()
	if err := RegisterRoutes(mux, RouterDeps{
		AuthService:      authService,
		AuthSecureCookie: false,
		AuditLogger:      auditLogger,
		Version:          "test",
		Metrics:          reg,
	}); err != nil {
		t.Fatalf("register routes failed: %v", err)
	}
	handler := Chain(mux, AuthMiddlewareFunc(authService, func() bool { return true }))

	loginRR := httptest.NewRecorder()
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader([]byte(`{"username":"admin","password":"wrong-password"}`)))
	handler.ServeHTTP(loginRR, loginReq)
	if loginRR.Code != http.StatusUnauthorized {
		t.Fatalf("expected invalid login to be rejected, got %d", loginRR.Code)
	}

	exported := reg.Export()
	if !strings.Contains(exported, `monsoon_auth_requests_total{endpoint="login",outcome="invalid_credentials"} 1.000000`) {
		t.Fatalf("expected invalid login metric to be exported, got %q", exported)
	}
	if !strings.Contains(exported, `monsoon_security_events_total{event="auth_failure",surface="login"} 1.000000`) {
		t.Fatalf("expected aggregated auth failure metric to be exported, got %q", exported)
	}

	entries, err := auditLogger.Query(context.Background(), audit.QueryFilter{
		Action: "auth.login.failed",
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("query audit log: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected invalid login audit entry, got %d", len(entries))
	}
	if entries[0].ObjectID != "admin" {
		t.Fatalf("expected invalid login audit object id to be username, got %+v", entries[0])
	}
}

func TestAuthLoginLocksAccountAfterRepeatedFailures(t *testing.T) {
	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{"users", "api_tokens", "api_tokens_by_hash", "audit"})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer eng.Close()

	authService := auth.NewService(eng, auth.ServiceOptions{
		CookieName:        "monsoon_session",
		SessionDuration:   time.Hour,
		MaxFailedAttempts: 2,
		LockoutDuration:   time.Minute,
	})
	if err := authService.BootstrapAdmin(context.Background(), "admin", "correct-pass"); err != nil {
		t.Fatalf("bootstrap admin: %v", err)
	}

	mux := http.NewServeMux()
	reg := metrics.NewRegistry()
	if err := RegisterRoutes(mux, RouterDeps{
		AuthService:      authService,
		AuthSecureCookie: false,
		AuditLogger:      audit.NewLogger(eng),
		Version:          "test",
		Metrics:          reg,
	}); err != nil {
		t.Fatalf("register routes failed: %v", err)
	}
	handler := Chain(mux, AuthMiddlewareFunc(authService, func() bool { return true }))

	firstRR := httptest.NewRecorder()
	firstReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader([]byte(`{"username":"admin","password":"wrong-pass"}`)))
	handler.ServeHTTP(firstRR, firstReq)
	if firstRR.Code != http.StatusUnauthorized {
		t.Fatalf("expected first failed login to be unauthorized, got %d", firstRR.Code)
	}

	secondRR := httptest.NewRecorder()
	secondReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader([]byte(`{"username":"admin","password":"wrong-pass"}`)))
	handler.ServeHTTP(secondRR, secondReq)
	if secondRR.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second failed login to lock account, got %d", secondRR.Code)
	}
	if secondRR.Header().Get("Retry-After") == "" {
		t.Fatalf("expected retry-after header when account is locked")
	}

	lockedRR := httptest.NewRecorder()
	lockedReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader([]byte(`{"username":"admin","password":"correct-pass"}`)))
	handler.ServeHTTP(lockedRR, lockedReq)
	if lockedRR.Code != http.StatusTooManyRequests {
		t.Fatalf("expected correct password to remain locked until timeout, got %d", lockedRR.Code)
	}

	exported := reg.Export()
	if !strings.Contains(exported, `monsoon_security_events_total{event="account_locked",surface="login"} 2.000000`) {
		t.Fatalf("expected aggregated account lock metric to be exported, got %q", exported)
	}

	auditLogger := audit.NewLogger(eng)
	failedEntries, err := auditLogger.Query(context.Background(), audit.QueryFilter{
		Action: "auth.login.failed",
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("query failed auth audit: %v", err)
	}
	if len(failedEntries) != 1 {
		t.Fatalf("expected one failed-login audit entry before lockout, got %d", len(failedEntries))
	}

	lockedEntries, err := auditLogger.Query(context.Background(), audit.QueryFilter{
		Action: "auth.login.locked",
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("query locked auth audit: %v", err)
	}
	if len(lockedEntries) < 1 {
		t.Fatalf("expected at least one lockout audit entry")
	}
	if lockedEntries[0].ObjectID != "admin" {
		t.Fatalf("expected locked audit entry to target admin, got %+v", lockedEntries[0])
	}
}

func TestAuthRoutesRejectUnsupportedLocalAuthMode(t *testing.T) {
	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{"users", "sessions", "api_tokens", "api_tokens_by_hash"})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer eng.Close()

	authService := auth.NewService(eng, auth.ServiceOptions{
		AuthType:        "ldap",
		CookieName:      "monsoon_session",
		SessionDuration: time.Hour,
	})

	mux := http.NewServeMux()
	if err := RegisterRoutes(mux, RouterDeps{
		AuthService:      authService,
		AuthSecureCookie: false,
		Version:          "test",
		Metrics:          metrics.NewRegistry(),
	}); err != nil {
		t.Fatalf("register routes failed: %v", err)
	}
	handler := Chain(mux, AuthMiddlewareFunc(authService, func() bool { return true }))

	for _, tc := range []struct {
		name string
		path string
		body string
	}{
		{name: "bootstrap", path: "/api/v1/auth/bootstrap", body: `{"username":"admin","password":"secret-pass"}`},
		{name: "login", path: "/api/v1/auth/login", body: `{"username":"admin","password":"secret-pass"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewReader([]byte(tc.body)))
			handler.ServeHTTP(rr, req)
			if rr.Code != http.StatusNotImplemented {
				t.Fatalf("expected unsupported auth mode to return 501, got %d body=%s", rr.Code, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), "auth_mode_unsupported") {
				t.Fatalf("expected auth_mode_unsupported response, got %s", rr.Body.String())
			}
		})
	}
}

func TestRequireRoleForMutationHonorsAuthEnforcement(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/subnets", nil)

	rr := httptest.NewRecorder()
	openReq := req.WithContext(WithAuthEnforcement(req.Context(), false))
	if !requireRoleForMutation(rr, openReq, auth.DefaultRoleOperator) {
		t.Fatalf("expected mutation to be allowed when auth is disabled")
	}

	rr = httptest.NewRecorder()
	protectedReq := req.WithContext(WithAuthEnforcement(req.Context(), true))
	if requireRoleForMutation(rr, protectedReq, auth.DefaultRoleOperator) {
		t.Fatalf("expected missing identity to be denied when auth is enforced")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	viewerCtx := withTestIdentity(WithAuthEnforcement(req.Context(), true), auth.Identity{Username: "viewer", Role: auth.DefaultRoleViewer})
	if requireRoleForMutation(rr, req.WithContext(viewerCtx), auth.DefaultRoleOperator) {
		t.Fatalf("expected viewer role to be denied")
	}
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	operatorCtx := withTestIdentity(WithAuthEnforcement(req.Context(), true), auth.Identity{Username: "operator", Role: auth.DefaultRoleOperator})
	if !requireRoleForMutation(rr, req.WithContext(operatorCtx), auth.DefaultRoleOperator) {
		t.Fatalf("expected operator role to be allowed")
	}
}

func TestAuditRoutesCaptureChanges(t *testing.T) {
	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{"subnets", "reservations", "leases", "audit"})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer eng.Close()

	leaseStore := lease.NewStore(eng)
	ipamEngine := ipam.NewEngine(eng, leaseStore)
	auditLogger := audit.NewLogger(eng)

	mux := http.NewServeMux()
	if err := RegisterRoutes(mux, RouterDeps{
		LeaseStore:  leaseStore,
		IPAMEngine:  ipamEngine,
		AuditLogger: auditLogger,
		Version:     "test",
	}); err != nil {
		t.Fatalf("register routes failed: %v", err)
	}

	subnetPayload := []byte(`{"cidr":"10.33.0.0/24","name":"Audit","vlan":33,"gateway":"10.33.0.1","dns":["10.33.0.2"],"dhcp_enabled":true,"pool_start":"10.33.0.10","pool_end":"10.33.0.50","lease_time_sec":3600}`)
	saveReq := httptest.NewRequest(http.MethodPost, "/api/v1/subnets", bytes.NewReader(subnetPayload))
	saveRR := httptest.NewRecorder()
	mux.ServeHTTP(saveRR, saveReq)
	if saveRR.Code != http.StatusOK {
		t.Fatalf("subnet upsert status mismatch: got %d body=%s", saveRR.Code, saveRR.Body.String())
	}

	auditReq := httptest.NewRequest(http.MethodGet, "/api/v1/audit?limit=20", nil)
	auditRR := httptest.NewRecorder()
	mux.ServeHTTP(auditRR, auditReq)
	if auditRR.Code != http.StatusOK {
		t.Fatalf("audit status mismatch: got %d body=%s", auditRR.Code, auditRR.Body.String())
	}

	var env Envelope
	if err := json.Unmarshal(auditRR.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode audit response: %v", err)
	}
	if env.Data == nil {
		t.Fatalf("expected audit data")
	}

	csvReq := httptest.NewRequest(http.MethodGet, "/api/v1/audit?format=csv", nil)
	csvRR := httptest.NewRecorder()
	mux.ServeHTTP(csvRR, csvReq)
	if csvRR.Code != http.StatusOK {
		t.Fatalf("audit csv export status mismatch: got %d body=%s", csvRR.Code, csvRR.Body.String())
	}
	if csvRR.Header().Get("Content-Type") == "" {
		t.Fatalf("expected csv content type")
	}
	if csvRR.Body.Len() == 0 {
		t.Fatalf("expected csv body")
	}
}

func TestDiscoveryRoutes(t *testing.T) {
	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{"subnets", "reservations", "leases", "discovery_scans", "discovery_meta"})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer eng.Close()

	leaseStore := lease.NewStore(eng)
	ipamEngine := ipam.NewEngine(eng, leaseStore)
	discoveryEngine := discovery.NewEngineWithOptions(eng, leaseStore, ipamEngine, time.Hour, discovery.Options{})

	mux := http.NewServeMux()
	if err := RegisterRoutes(mux, RouterDeps{
		LeaseStore:      leaseStore,
		IPAMEngine:      ipamEngine,
		DiscoveryEngine: discoveryEngine,
		Version:         "test",
	}); err != nil {
		t.Fatalf("register routes failed: %v", err)
	}

	statusRR := httptest.NewRecorder()
	statusReq := httptest.NewRequest(http.MethodGet, "/api/v1/discovery/status", nil)
	mux.ServeHTTP(statusRR, statusReq)
	if statusRR.Code != http.StatusOK {
		t.Fatalf("status code mismatch: %d", statusRR.Code)
	}

	scanRR := httptest.NewRecorder()
	scanReq := httptest.NewRequest(http.MethodPost, "/api/v1/discovery/scan", nil)
	mux.ServeHTTP(scanRR, scanReq)
	if scanRR.Code != http.StatusAccepted {
		t.Fatalf("scan code mismatch: %d", scanRR.Code)
	}
}

func TestSystemRoutesInfoConfigAndBackups(t *testing.T) {
	mux := http.NewServeMux()
	startedAt := time.Now().UTC().Add(-5 * time.Minute)
	if err := RegisterRoutes(mux, RouterDeps{
		Version:   "test",
		StartedAt: startedAt,
		ReloadStatus: func() any {
			return map[string]any{
				"last_reloaded_at":         startedAt,
				"hot_reloadable":           []string{"api.rest.cors_origins"},
				"restart_required":         []string{"api.rest.listen"},
				"restart_required_pending": true,
			}
		},
		ConfigSnapshot: func() any {
			return map[string]any{
				"auth": map[string]any{
					"enabled":       true,
					"password":      "top-secret",
					"token":         "visible-value",
					"client_secret": "oauth-secret",
				},
			}
		},
		UpdateConfig: func(_ context.Context, payload map[string]any) (any, error) {
			return payload, nil
		},
		CreateBackup: func(context.Context) (SystemBackup, error) {
			return SystemBackup{
				Name:      "monsoon-test.snapshot",
				Path:      "/tmp/monsoon-test.snapshot",
				SizeBytes: 321,
				CreatedAt: time.Now().UTC(),
			}, nil
		},
		ListBackups: func(context.Context) ([]SystemBackup, error) {
			return []SystemBackup{{
				Name:      "monsoon-existing.snapshot",
				Path:      "/tmp/monsoon-existing.snapshot",
				SizeBytes: 654,
				CreatedAt: time.Now().UTC(),
			}}, nil
		},
		RestoreBackup: func(_ context.Context, req RestoreRequest) (SystemBackup, error) {
			return SystemBackup{
				Name:      req.Name,
				Path:      "/tmp/" + req.Name,
				SizeBytes: 654,
				CreatedAt: time.Now().UTC(),
			}, nil
		},
	}); err != nil {
		t.Fatalf("register routes failed: %v", err)
	}

	infoRR := httptest.NewRecorder()
	infoReq := httptest.NewRequest(http.MethodGet, "/api/v1/system/info", nil)
	mux.ServeHTTP(infoRR, infoReq)
	if infoRR.Code != http.StatusOK {
		t.Fatalf("system info status mismatch: got %d body=%s", infoRR.Code, infoRR.Body.String())
	}
	infoData := decodeResponseMap(t, infoRR.Body.Bytes())
	reloadInfo, _ := infoData["config_reload"].(map[string]any)
	if pending, _ := reloadInfo["restart_required_pending"].(bool); !pending {
		t.Fatalf("expected restart-required flag in system info, got %#v", reloadInfo)
	}

	configRR := httptest.NewRecorder()
	configReq := httptest.NewRequest(http.MethodGet, "/api/v1/system/config", nil)
	mux.ServeHTTP(configRR, configReq)
	if configRR.Code != http.StatusOK {
		t.Fatalf("system config status mismatch: got %d body=%s", configRR.Code, configRR.Body.String())
	}
	if !bytes.Contains(configRR.Body.Bytes(), []byte(`"password":"***"`)) {
		t.Fatalf("expected masked password in config response: %s", configRR.Body.String())
	}
	if !bytes.Contains(configRR.Body.Bytes(), []byte(`"token":"***"`)) {
		t.Fatalf("expected masked token in config response: %s", configRR.Body.String())
	}
	if bytes.Contains(configRR.Body.Bytes(), []byte("visible-value")) || bytes.Contains(configRR.Body.Bytes(), []byte("oauth-secret")) {
		t.Fatalf("expected secrets to be removed from config response: %s", configRR.Body.String())
	}
	configMeta := decodeResponseMetaMap(t, configRR.Body.Bytes())
	if _, ok := configMeta["reload"].(map[string]any); !ok {
		t.Fatalf("expected reload metadata in config response: %#v", configMeta)
	}

	exportRR := httptest.NewRecorder()
	exportReq := httptest.NewRequest(http.MethodGet, "/api/v1/system/config/export?format=yaml", nil)
	mux.ServeHTTP(exportRR, exportReq)
	if exportRR.Code != http.StatusOK {
		t.Fatalf("system config export status mismatch: got %d body=%s", exportRR.Code, exportRR.Body.String())
	}
	if exportRR.Body.Len() == 0 {
		t.Fatalf("expected export body")
	}
	if bytes.Contains(exportRR.Body.Bytes(), []byte("visible-value")) || bytes.Contains(exportRR.Body.Bytes(), []byte("oauth-secret")) {
		t.Fatalf("expected secrets to be removed from export response: %s", exportRR.Body.String())
	}

	updateRR := httptest.NewRecorder()
	updateReq := httptest.NewRequest(http.MethodPut, "/api/v1/system/config", bytes.NewReader([]byte(`{"auth":{"password":"new-secret"},"api":{"rest":{"listen":":19067"}}}`)))
	mux.ServeHTTP(updateRR, updateReq)
	if updateRR.Code != http.StatusOK {
		t.Fatalf("system config update status mismatch: got %d body=%s", updateRR.Code, updateRR.Body.String())
	}
	if !bytes.Contains(updateRR.Body.Bytes(), []byte(`"password":"***"`)) {
		t.Fatalf("expected masked password in update response: %s", updateRR.Body.String())
	}
	updateMeta := decodeResponseMetaMap(t, updateRR.Body.Bytes())
	reloadMeta, _ := updateMeta["reload"].(map[string]any)
	if pending, _ := reloadMeta["restart_required_pending"].(bool); !pending {
		t.Fatalf("expected reload metadata after config update: %#v", reloadMeta)
	}

	listRR := httptest.NewRecorder()
	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/system/backups", nil)
	mux.ServeHTTP(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("system backups list status mismatch: got %d body=%s", listRR.Code, listRR.Body.String())
	}

	createRR := httptest.NewRecorder()
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/system/backup", nil)
	mux.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusOK {
		t.Fatalf("system backup create status mismatch: got %d body=%s", createRR.Code, createRR.Body.String())
	}

	restoreRR := httptest.NewRecorder()
	restoreReq := httptest.NewRequest(http.MethodPost, "/api/v1/system/restore", bytes.NewReader([]byte(`{"name":"monsoon-existing.snapshot"}`)))
	mux.ServeHTTP(restoreRR, restoreReq)
	if restoreRR.Code != http.StatusOK {
		t.Fatalf("system restore status mismatch: got %d body=%s", restoreRR.Code, restoreRR.Body.String())
	}
}

func TestSystemHealthAndReadyRoutesReflectReadiness(t *testing.T) {
	mux := http.NewServeMux()
	if err := RegisterRoutes(mux, RouterDeps{
		Version:          "test",
		StartedAt:        time.Now().UTC().Add(-2 * time.Minute),
		DiscoveryEnabled: true,
		StorageReady: func(context.Context) error {
			return nil
		},
		DHCPv4Enabled: true,
		DHCPv4Listen:  ":67",
		DHCPv4Running: func() bool {
			return false
		},
		HAEnabled: true,
		HAStatus: func() any {
			return map[string]any{
				"node":   "alpha",
				"role":   "primary",
				"peer":   "disconnected",
				"fenced": false,
			}
		},
	}); err != nil {
		t.Fatalf("register routes failed: %v", err)
	}

	healthRR := httptest.NewRecorder()
	healthReq := httptest.NewRequest(http.MethodGet, "/api/v1/system/health", nil)
	mux.ServeHTTP(healthRR, healthReq)
	if healthRR.Code != http.StatusOK {
		t.Fatalf("health status mismatch: got %d body=%s", healthRR.Code, healthRR.Body.String())
	}
	health := decodeResponseMap(t, healthRR.Body.Bytes())
	if health["status"] != "degraded" {
		t.Fatalf("expected degraded health status, got %#v", health["status"])
	}
	if ready, _ := health["ready"].(bool); ready {
		t.Fatalf("expected overall readiness false in health payload: %#v", health)
	}
	components, _ := health["components"].(map[string]any)
	storageComponent, _ := components["storage"].(map[string]any)
	if storageComponent["status"] != "up" {
		t.Fatalf("expected storage component up, got %#v", storageComponent)
	}
	dhcpComponent, _ := components["dhcpv4"].(map[string]any)
	if dhcpComponent["status"] != "down" {
		t.Fatalf("expected dhcpv4 component down, got %#v", dhcpComponent)
	}

	readyRR := httptest.NewRecorder()
	readyReq := httptest.NewRequest(http.MethodGet, "/api/v1/system/ready", nil)
	mux.ServeHTTP(readyRR, readyReq)
	if readyRR.Code != http.StatusServiceUnavailable {
		t.Fatalf("ready status mismatch: got %d body=%s", readyRR.Code, readyRR.Body.String())
	}
}

func TestSystemSensitiveReadsRequireAdminWhenAuthIsEnforced(t *testing.T) {
	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{"audit"})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer eng.Close()

	mux := http.NewServeMux()
	if err := RegisterRoutes(mux, RouterDeps{
		Version:     "test",
		AuditLogger: audit.NewLogger(eng),
		ConfigSnapshot: func() any {
			return map[string]any{
				"auth": map[string]any{"token": "visible-value"},
			}
		},
		ListBackups: func(context.Context) ([]SystemBackup, error) {
			return []SystemBackup{{
				Name:      "monsoon-existing.snapshot",
				Path:      "/tmp/monsoon-existing.snapshot",
				SizeBytes: 654,
				CreatedAt: time.Now().UTC(),
			}}, nil
		},
	}); err != nil {
		t.Fatalf("register routes failed: %v", err)
	}

	viewerCtx := withTestIdentity(context.Background(), auth.Identity{Username: "viewer", Role: auth.DefaultRoleViewer})
	viewerCtx = WithAuthEnforcement(viewerCtx, true)
	adminCtx := withTestIdentity(context.Background(), auth.Identity{Username: "admin", Role: auth.DefaultRoleAdmin})
	adminCtx = WithAuthEnforcement(adminCtx, true)

	for _, tc := range []string{
		"/api/v1/system/config",
		"/api/v1/system/config/export?format=json",
		"/api/v1/system/backups",
		"/api/v1/audit",
	} {
		t.Run(tc, func(t *testing.T) {
			viewerRR := httptest.NewRecorder()
			viewerReq := httptest.NewRequest(http.MethodGet, tc, nil).WithContext(viewerCtx)
			mux.ServeHTTP(viewerRR, viewerReq)
			if viewerRR.Code != http.StatusForbidden {
				t.Fatalf("expected viewer request to be forbidden for %s, got %d body=%s", tc, viewerRR.Code, viewerRR.Body.String())
			}

			adminRR := httptest.NewRecorder()
			adminReq := httptest.NewRequest(http.MethodGet, tc, nil).WithContext(adminCtx)
			mux.ServeHTTP(adminRR, adminReq)
			if adminRR.Code != http.StatusOK {
				t.Fatalf("expected admin request to succeed for %s, got %d body=%s", tc, adminRR.Code, adminRR.Body.String())
			}
		})
	}
}

func TestParseBoundedPositiveLimit(t *testing.T) {
	t.Run("uses default when empty", func(t *testing.T) {
		limit, err := parseBoundedPositiveLimit("", 100, 500)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if limit != 100 {
			t.Fatalf("expected default limit 100, got %d", limit)
		}
	})

	t.Run("rejects non-positive values", func(t *testing.T) {
		if _, err := parseBoundedPositiveLimit("0", 100, 500); err == nil {
			t.Fatalf("expected zero limit to be rejected")
		}
	})

	t.Run("caps oversized values", func(t *testing.T) {
		limit, err := parseBoundedPositiveLimit("5000", 100, 500)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if limit != 500 {
			t.Fatalf("expected limit cap 500, got %d", limit)
		}
	})
}

func TestSystemReadyRouteReturnsOKWhenCoreComponentsAreReady(t *testing.T) {
	mux := http.NewServeMux()
	if err := RegisterRoutes(mux, RouterDeps{
		Version:   "test",
		StartedAt: time.Now().UTC().Add(-time.Minute),
		StorageReady: func(context.Context) error {
			return nil
		},
		DHCPv4Enabled: false,
		HAEnabled:     true,
		HAStatus: func() any {
			return map[string]any{
				"node":   "alpha",
				"role":   "primary",
				"peer":   "connected",
				"fenced": false,
			}
		},
	}); err != nil {
		t.Fatalf("register routes failed: %v", err)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/ready", nil)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected ready route to return 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	readyPayload := decodeResponseMap(t, rr.Body.Bytes())
	if readyPayload["status"] != "healthy" {
		t.Fatalf("expected healthy ready payload, got %#v", readyPayload["status"])
	}
	if ready, _ := readyPayload["ready"].(bool); !ready {
		t.Fatalf("expected ready route to report ready=true: %#v", readyPayload)
	}
}

func decodeResponseMap(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var env Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode response envelope: %v", err)
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map response data, got %#v", env.Data)
	}
	return data
}

func decodeResponseMetaMap(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var env Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode response envelope: %v", err)
	}
	meta, ok := env.Meta.(map[string]any)
	if !ok {
		t.Fatalf("expected map response meta, got %#v", env.Meta)
	}
	return meta
}

func TestHARoutesStatusAndManualFailover(t *testing.T) {
	mux := http.NewServeMux()
	if err := RegisterRoutes(mux, RouterDeps{
		Version: "test",
		HAStatus: func() any {
			return map[string]any{
				"node":      "alpha",
				"mode":      "active-passive",
				"role":      "primary",
				"peer":      "connected",
				"peer_node": "beta",
			}
		},
		HATriggerFailover: func(_ context.Context, reason string) (any, error) {
			return map[string]any{
				"status": "accepted",
				"reason": reason,
				"role":   "secondary",
			}, nil
		},
	}); err != nil {
		t.Fatalf("register routes failed: %v", err)
	}

	statusRR := httptest.NewRecorder()
	statusReq := httptest.NewRequest(http.MethodGet, "/api/v1/ha/status", nil)
	mux.ServeHTTP(statusRR, statusReq)
	if statusRR.Code != http.StatusOK {
		t.Fatalf("ha status mismatch: got %d body=%s", statusRR.Code, statusRR.Body.String())
	}
	if !bytes.Contains(statusRR.Body.Bytes(), []byte(`"role":"primary"`)) {
		t.Fatalf("expected primary role in ha status: %s", statusRR.Body.String())
	}

	failoverRR := httptest.NewRecorder()
	failoverReq := httptest.NewRequest(http.MethodPost, "/api/v1/ha/failover", bytes.NewReader([]byte(`{"reason":"maintenance"}`)))
	mux.ServeHTTP(failoverRR, failoverReq)
	if failoverRR.Code != http.StatusAccepted {
		t.Fatalf("ha failover mismatch: got %d body=%s", failoverRR.Code, failoverRR.Body.String())
	}
	if !bytes.Contains(failoverRR.Body.Bytes(), []byte(`"reason":"maintenance"`)) {
		t.Fatalf("expected manual failover reason in response: %s", failoverRR.Body.String())
	}
}
