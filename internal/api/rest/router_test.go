package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/monsoondhcp/monsoon/internal/audit"
	"github.com/monsoondhcp/monsoon/internal/auth"
	"github.com/monsoondhcp/monsoon/internal/discovery"
	"github.com/monsoondhcp/monsoon/internal/ipam"
	"github.com/monsoondhcp/monsoon/internal/lease"
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
	if err := authService.EnsureAdmin(context.Background(), "admin", ""); err != nil {
		t.Fatalf("ensure admin: %v", err)
	}

	mux := http.NewServeMux()
	if err := RegisterRoutes(mux, RouterDeps{
		AuthService:      authService,
		AuthSecureCookie: false,
		Version:          "test",
	}); err != nil {
		t.Fatalf("register routes failed: %v", err)
	}
	handler := Chain(mux, AuthMiddleware(authService, true))

	loginRR := httptest.NewRecorder()
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader([]byte(`{"username":"admin","password":"admin"}`)))
	handler.ServeHTTP(loginRR, loginReq)
	if loginRR.Code != http.StatusOK {
		t.Fatalf("login status mismatch: got %d body=%s", loginRR.Code, loginRR.Body.String())
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
	discoveryEngine := discovery.NewEngine(eng, leaseStore, ipamEngine, time.Hour)

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
