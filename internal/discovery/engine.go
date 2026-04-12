package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/monsoondhcp/monsoon/internal/ipam"
	"github.com/monsoondhcp/monsoon/internal/lease"
	"github.com/monsoondhcp/monsoon/internal/storage"
)

const (
	treeDiscoveryScans = "discovery_scans"
	treeDiscoveryMeta  = "discovery_meta"
	keyLatestHosts     = "latest_hosts"
	keyLatestRogue     = "latest_rogue"
	keyLatestScanID    = "latest_scan_id"
	keyLastScanAt      = "last_scan_at"
)

type Options struct {
	Methods             []string
	TCPPorts            []int
	ProbeTimeout        time.Duration
	MaxTargetsPerSubnet int
	MaxConcurrency      int
}

type Engine struct {
	store      *storage.Engine
	leaseStore lease.Store
	ipamEngine *ipam.Engine
	interval   time.Duration
	options    Options

	mu          sync.RWMutex
	scanning    bool
	lastScanAt  time.Time
	nextScanAt  time.Time
	latestID    string
	latestRogue []RogueServer
	progress    Progress
	onComplete  func(ScanResult)
}

type knownHost struct {
	ip       string
	mac      string
	hostname string
	subnet   string
	source   string
}

type probeOutcome struct {
	alive    bool
	method   string
	hostname string
}

var (
	ipv4Pattern = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	macPattern  = regexp.MustCompile(`(?i)\b[0-9a-f]{2}[:-](?:[0-9a-f]{2}[:-]){4}[0-9a-f]{2}\b`)
)

func DefaultOptions() Options {
	return Options{
		Methods:             []string{"passive"},
		TCPPorts:            []int{22, 80, 443},
		ProbeTimeout:        900 * time.Millisecond,
		MaxTargetsPerSubnet: 32,
		MaxConcurrency:      12,
	}
}

func NormalizeOptions(in Options) Options {
	out := in
	if len(out.Methods) == 0 {
		out.Methods = []string{"passive"}
	}
	out.Methods = normalizeMethods(out.Methods)
	if len(out.TCPPorts) == 0 {
		out.TCPPorts = []int{22, 80, 443}
	}
	if out.ProbeTimeout <= 0 {
		out.ProbeTimeout = 900 * time.Millisecond
	}
	if out.MaxTargetsPerSubnet <= 0 {
		out.MaxTargetsPerSubnet = 32
	}
	if out.MaxConcurrency <= 0 {
		out.MaxConcurrency = 12
	}
	return out
}

func NewEngine(store *storage.Engine, leaseStore lease.Store, ipamEngine *ipam.Engine, interval time.Duration) *Engine {
	return NewEngineWithOptions(store, leaseStore, ipamEngine, interval, DefaultOptions())
}

func NewEngineWithOptions(store *storage.Engine, leaseStore lease.Store, ipamEngine *ipam.Engine, interval time.Duration, options Options) *Engine {
	if interval <= 0 {
		interval = time.Hour
	}
	e := &Engine{
		store:      store,
		leaseStore: leaseStore,
		ipamEngine: ipamEngine,
		interval:   interval,
		options:    NormalizeOptions(options),
		progress: Progress{
			Phase: "idle",
		},
	}
	e.loadMeta()
	return e
}

func (e *Engine) Start(ctx context.Context) {
	e.mu.Lock()
	if e.nextScanAt.IsZero() {
		e.nextScanAt = time.Now().UTC().Add(e.interval)
	}
	e.mu.Unlock()

	ticker := time.NewTicker(e.interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = e.TriggerScan(context.Background(), ScanRequest{Reason: "scheduled"})
			}
		}
	}()
}

func (e *Engine) Status(_ context.Context) Status {
	e.mu.RLock()
	defer e.mu.RUnlock()

	status := Status{
		SensorOnline:      true,
		LastScanAt:        e.lastScanAt,
		NextScheduledScan: e.nextScanAt,
		Scanning:          e.scanning,
		LatestScanID:      e.latestID,
		Progress:          e.progress,
	}
	status.RogueDetected = len(e.latestRogue) > 0
	if latest, err := e.LatestResult(context.Background()); err == nil {
		status.ActiveConflicts = len(latest.Conflicts)
	}
	return status
}

func (e *Engine) Progress(_ context.Context) Progress {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.progress
}

func (e *Engine) SetOnComplete(fn func(ScanResult)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onComplete = fn
}

func (e *Engine) TriggerScan(_ context.Context, request ScanRequest) (string, error) {
	e.mu.Lock()
	if e.scanning {
		e.mu.Unlock()
		return "", fmt.Errorf("scan already in progress")
	}
	e.scanning = true
	now := time.Now().UTC()
	e.progress = Progress{
		Phase:      "initializing",
		Total:      0,
		Processed:  0,
		Percent:    0,
		StartedAt:  now,
		UpdatedAt:  now,
		InProgress: true,
	}
	e.mu.Unlock()

	scanID := time.Now().UTC().Format("20060102-150405.000000000")
	e.updateProgress(func(p *Progress) {
		p.ScanID = scanID
		p.Phase = "collecting"
	})
	go e.runScan(scanID, request)
	return scanID, nil
}

func (e *Engine) runScan(scanID string, request ScanRequest) {
	started := time.Now().UTC()
	defer e.markScanDone()

	result := ScanResult{
		ScanID:    scanID,
		Status:    "running",
		Reason:    normalizeReason(request.Reason),
		Subnets:   append([]string(nil), request.Subnets...),
		StartedAt: started,
	}

	leases := []lease.Lease{}
	if e.leaseStore != nil {
		leases, _ = e.leaseStore.ListAll(context.Background())
	}
	reservations := []ipam.Reservation{}
	if e.ipamEngine != nil {
		reservations, _ = e.ipamEngine.ListReservations(context.Background())
	}
	subnets := e.resolveTargetSubnets(request.Subnets)
	previousHosts := filterObservedHostsForSubnets(e.loadPreviousHosts(), subnets)
	if len(result.Subnets) == 0 && len(subnets) > 0 {
		result.Subnets = make([]string, 0, len(subnets))
		for _, subnet := range subnets {
			result.Subnets = append(result.Subnets, subnet.CIDR)
		}
	}

	knownByIP := filterKnownHostsForSubnets(collectKnownHosts(leases, reservations), subnets)
	if containsMethod(e.options.Methods, "arp") {
		for ip, mac := range readARPNeighbors() {
			if !ipMatchesSubnets(ip, "", subnets) {
				continue
			}
			item := knownByIP[ip]
			if item.ip == "" {
				item = knownHost{ip: ip, source: "arp"}
			}
			if item.mac == "" {
				item.mac = mac
			}
			knownByIP[ip] = item
		}
	}
	conflictMap := filterConflictMapForSubnets(collectConflictMap(leases, reservations), subnets)
	targets := e.buildTargets(subnets, knownByIP, previousHosts)
	e.updateProgress(func(p *Progress) {
		p.Phase = "probing"
		p.Total = len(targets)
		p.Processed = 0
		p.Percent = 0
	})
	outcomes := e.probeTargets(targets)
	e.updateProgress(func(p *Progress) {
		p.Phase = "analyzing"
		p.Processed = p.Total
		p.Percent = 85
	})

	current := map[string]ObservedHost{}
	now := time.Now().UTC()

	for _, ip := range targets {
		base, known := knownByIP[ip]
		outcome := outcomes[ip]
		if !known && !outcome.alive {
			continue
		}
		state := deriveState(ip, known, base, outcome, previousHosts)
		current[ip] = ObservedHost{
			IP:       ip,
			MAC:      base.mac,
			Vendor:   LookupVendor(base.mac),
			Hostname: pickHostname(base.hostname, outcome.hostname),
			Subnet:   base.subnet,
			State:    state,
			SeenAt:   now,
		}
	}

	for ip, prev := range previousHosts {
		if _, ok := current[ip]; ok {
			continue
		}
		current[ip] = ObservedHost{
			IP:       ip,
			MAC:      prev.MAC,
			Vendor:   prev.Vendor,
			Hostname: prev.Hostname,
			Subnet:   prev.Subnet,
			State:    "missing",
			SeenAt:   now,
		}
	}

	conflicts := make([]Conflict, 0, 8)
	for ip, macSet := range conflictMap {
		if len(macSet) <= 1 {
			continue
		}
		macs := make([]string, 0, len(macSet))
		for mac := range macSet {
			macs = append(macs, mac)
		}
		sort.Strings(macs)
		conflicts = append(conflicts, Conflict{
			IP:       ip,
			MACs:     macs,
			Severity: "high",
			Note:     conflictNoteForMACs(macs),
		})
	}
	sort.Slice(conflicts, func(i, j int) bool { return conflicts[i].IP < conflicts[j].IP })

	hosts := make([]ObservedHost, 0, len(current))
	for _, host := range current {
		hosts = append(hosts, host)
		switch host.State {
		case "new":
			result.NewHosts++
		case "changed":
			result.ChangedHosts++
		case "missing":
			result.MissingHosts++
		default:
			result.KnownHosts++
		}
	}
	sort.Slice(hosts, func(i, j int) bool { return compareIPString(hosts[i].IP, hosts[j].IP) < 0 })

	result.Hosts = hosts
	result.Conflicts = conflicts
	result.RogueServers = e.snapshotRogueServers()
	result.TotalHosts = len(hosts)
	result.CompletedAt = time.Now().UTC()
	result.DurationMS = result.CompletedAt.Sub(started).Milliseconds()
	result.Status = "completed"
	e.updateProgress(func(p *Progress) {
		p.Phase = "persisting"
		p.Percent = 95
	})

	_ = e.persistResult(result)
	e.updateProgress(func(p *Progress) {
		p.Phase = "completed"
		p.Percent = 100
		p.InProgress = false
		p.UpdatedAt = time.Now().UTC()
	})
	e.mu.RLock()
	onComplete := e.onComplete
	e.mu.RUnlock()
	if onComplete != nil {
		onComplete(result)
	}
}

func (e *Engine) markScanDone() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.scanning = false
	if e.progress.InProgress {
		e.progress.InProgress = false
		if e.progress.Phase != "completed" {
			e.progress.Phase = "idle"
		}
		e.progress.UpdatedAt = time.Now().UTC()
	}
	if e.nextScanAt.Before(time.Now().UTC()) {
		e.nextScanAt = time.Now().UTC().Add(e.interval)
	}
}

func (e *Engine) updateProgress(mut func(p *Progress)) {
	if mut == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	mut(&e.progress)
	e.progress.UpdatedAt = time.Now().UTC()
}

func (e *Engine) resolveTargetSubnets(explicit []string) []ipam.Subnet {
	if e.ipamEngine == nil {
		return nil
	}
	subnets, err := e.ipamEngine.ListSubnets(context.Background())
	if err != nil {
		return nil
	}
	if len(explicit) == 0 {
		return subnets
	}
	lookup := map[string]struct{}{}
	for _, cidr := range explicit {
		c := strings.TrimSpace(cidr)
		if c != "" {
			lookup[c] = struct{}{}
		}
	}
	filtered := make([]ipam.Subnet, 0, len(subnets))
	for _, subnet := range subnets {
		if _, ok := lookup[subnet.CIDR]; ok {
			filtered = append(filtered, subnet)
		}
	}
	return filtered
}

func collectKnownHosts(leases []lease.Lease, reservations []ipam.Reservation) map[string]knownHost {
	out := map[string]knownHost{}
	for _, l := range leases {
		ip := strings.TrimSpace(l.IP)
		if ip == "" {
			continue
		}
		out[ip] = knownHost{
			ip:       ip,
			mac:      strings.ToUpper(strings.TrimSpace(l.MAC)),
			hostname: strings.TrimSpace(l.Hostname),
			subnet:   strings.TrimSpace(l.SubnetID),
			source:   "lease",
		}
	}
	for _, r := range reservations {
		ip := strings.TrimSpace(r.IP)
		if ip == "" {
			continue
		}
		item := out[ip]
		if item.ip == "" {
			item = knownHost{ip: ip, source: "reservation"}
		}
		if item.mac == "" {
			item.mac = strings.ToUpper(strings.TrimSpace(r.MAC))
		}
		if item.hostname == "" {
			item.hostname = strings.TrimSpace(r.Hostname)
		}
		if item.subnet == "" {
			item.subnet = strings.TrimSpace(r.SubnetCIDR)
		}
		out[ip] = item
	}
	return out
}

func collectConflictMap(leases []lease.Lease, reservations []ipam.Reservation) map[string]map[string]struct{} {
	conflicts := map[string]map[string]struct{}{}
	add := func(ip, mac string) {
		if ip == "" || mac == "" {
			return
		}
		set, ok := conflicts[ip]
		if !ok {
			set = map[string]struct{}{}
			conflicts[ip] = set
		}
		set[mac] = struct{}{}
	}
	for _, l := range leases {
		add(strings.TrimSpace(l.IP), strings.ToUpper(strings.TrimSpace(l.MAC)))
	}
	for _, r := range reservations {
		add(strings.TrimSpace(r.IP), strings.ToUpper(strings.TrimSpace(r.MAC)))
	}
	return conflicts
}

func filterConflictMapForSubnets(conflicts map[string]map[string]struct{}, subnets []ipam.Subnet) map[string]map[string]struct{} {
	if len(subnets) == 0 {
		return conflicts
	}
	filtered := make(map[string]map[string]struct{}, len(conflicts))
	for ip, macs := range conflicts {
		if ipMatchesSubnets(ip, "", subnets) {
			filtered[ip] = macs
		}
	}
	return filtered
}

func filterKnownHostsForSubnets(known map[string]knownHost, subnets []ipam.Subnet) map[string]knownHost {
	if len(subnets) == 0 {
		return known
	}
	filtered := make(map[string]knownHost, len(known))
	for ip, host := range known {
		if ipMatchesSubnets(ip, host.subnet, subnets) {
			filtered[ip] = host
		}
	}
	return filtered
}

func filterObservedHostsForSubnets(hosts map[string]ObservedHost, subnets []ipam.Subnet) map[string]ObservedHost {
	if len(subnets) == 0 {
		return hosts
	}
	filtered := make(map[string]ObservedHost, len(hosts))
	for ip, host := range hosts {
		if ipMatchesSubnets(ip, host.Subnet, subnets) {
			filtered[ip] = host
		}
	}
	return filtered
}

func ipMatchesSubnets(ip string, subnet string, subnets []ipam.Subnet) bool {
	if len(subnets) == 0 {
		return true
	}
	candidateIP, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		candidateIP = netip.Addr{}
	}
	for _, item := range subnets {
		if strings.TrimSpace(subnet) != "" && strings.EqualFold(strings.TrimSpace(subnet), item.CIDR) {
			return true
		}
		prefix, err := netip.ParsePrefix(strings.TrimSpace(item.CIDR))
		if err != nil || !candidateIP.IsValid() {
			continue
		}
		if prefix.Contains(candidateIP) {
			return true
		}
	}
	return false
}

func (e *Engine) buildTargets(subnets []ipam.Subnet, known map[string]knownHost, previous map[string]ObservedHost) []string {
	targetSet := map[string]struct{}{}
	for ip := range known {
		targetSet[ip] = struct{}{}
	}
	for ip := range previous {
		targetSet[ip] = struct{}{}
	}

	if hasActiveProbe(e.options.Methods) {
		for _, subnet := range subnets {
			start, end, ok := poolBounds(subnet)
			if !ok {
				continue
			}
			count := 0
			for addr := start; ; addr = addr.Next() {
				targetSet[addr.String()] = struct{}{}
				count++
				if addr.Compare(end) == 0 || count >= e.options.MaxTargetsPerSubnet {
					break
				}
			}
		}
	}

	out := make([]string, 0, len(targetSet))
	for ip := range targetSet {
		out = append(out, ip)
	}
	sort.Slice(out, func(i, j int) bool { return compareIPString(out[i], out[j]) < 0 })
	return out
}

func poolBounds(subnet ipam.Subnet) (netip.Addr, netip.Addr, bool) {
	if !subnet.DHCP.Enabled {
		return netip.Addr{}, netip.Addr{}, false
	}
	start, err := netip.ParseAddr(strings.TrimSpace(subnet.DHCP.PoolStart))
	if err != nil {
		return netip.Addr{}, netip.Addr{}, false
	}
	end, err := netip.ParseAddr(strings.TrimSpace(subnet.DHCP.PoolEnd))
	if err != nil {
		return netip.Addr{}, netip.Addr{}, false
	}
	if !start.Is4() || !end.Is4() || start.Compare(end) > 0 {
		return netip.Addr{}, netip.Addr{}, false
	}
	return start, end, true
}

func (e *Engine) probeTargets(targets []string) map[string]probeOutcome {
	if len(targets) == 0 {
		return map[string]probeOutcome{}
	}
	out := make(map[string]probeOutcome, len(targets))
	if !hasActiveProbe(e.options.Methods) && !containsMethod(e.options.Methods, "dns") {
		return out
	}

	type result struct {
		ip      string
		outcome probeOutcome
	}
	sem := make(chan struct{}, e.options.MaxConcurrency)
	resCh := make(chan result, len(targets))
	var wg sync.WaitGroup
	for _, ip := range targets {
		wg.Add(1)
		go func(target string) {
			defer wg.Done()
			sem <- struct{}{}
			outcome := e.probeIP(target)
			<-sem
			resCh <- result{ip: target, outcome: outcome}
		}(ip)
	}
	wg.Wait()
	close(resCh)
	for item := range resCh {
		out[item.ip] = item.outcome
		e.updateProgress(func(p *Progress) {
			if p.Total <= 0 {
				return
			}
			p.Processed++
			if p.Processed > p.Total {
				p.Processed = p.Total
			}
			pct := int(float64(p.Processed) / float64(p.Total) * 80.0)
			if pct < 1 && p.Processed > 0 {
				pct = 1
			}
			if pct > 80 {
				pct = 80
			}
			p.Percent = pct
		})
	}
	return out
}

func (e *Engine) probeIP(ip string) probeOutcome {
	out := probeOutcome{}
	if containsMethod(e.options.Methods, "ping") {
		if pingHost(ip, e.options.ProbeTimeout) {
			out.alive = true
			out.method = "ping"
		}
	}
	if !out.alive && containsMethod(e.options.Methods, "tcp") {
		if tcpReachable(ip, e.options.TCPPorts, e.options.ProbeTimeout) {
			out.alive = true
			out.method = "tcp"
		}
	}
	if containsMethod(e.options.Methods, "dns") {
		if names, err := net.LookupAddr(ip); err == nil && len(names) > 0 {
			out.hostname = strings.TrimSuffix(strings.TrimSpace(names[0]), ".")
			if out.method == "" {
				out.method = "dns"
			}
		}
	}
	return out
}

func deriveState(ip string, known bool, base knownHost, outcome probeOutcome, previous map[string]ObservedHost) string {
	if prev, ok := previous[ip]; ok {
		if prev.MAC != "" && base.mac != "" && !strings.EqualFold(prev.MAC, base.mac) {
			return "changed"
		}
	}
	if !known {
		if outcome.alive {
			return "new"
		}
		return "unknown"
	}
	if outcome.method == "" {
		return "known"
	}
	if outcome.alive {
		return "known"
	}
	return "missing"
}

func pickHostname(base string, probe string) string {
	if strings.TrimSpace(base) != "" {
		return base
	}
	return strings.TrimSpace(probe)
}

func (e *Engine) snapshotRogueServers() []RogueServer {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if len(e.latestRogue) == 0 {
		return []RogueServer{}
	}
	out := make([]RogueServer, len(e.latestRogue))
	copy(out, e.latestRogue)
	return out
}

func conflictNoteForMACs(macs []string) string {
	vendors := make([]string, 0, len(macs))
	seen := map[string]struct{}{}
	for _, mac := range macs {
		vendor := LookupVendor(mac)
		if vendor == "" {
			continue
		}
		if _, ok := seen[vendor]; ok {
			continue
		}
		seen[vendor] = struct{}{}
		vendors = append(vendors, vendor)
	}
	if len(vendors) == 0 {
		return "multiple MAC addresses observed for the same IP"
	}
	return "multiple MAC addresses observed for the same IP (" + strings.Join(vendors, ", ") + ")"
}

func hasActiveProbe(methods []string) bool {
	return containsMethod(methods, "ping") || containsMethod(methods, "tcp") || containsMethod(methods, "arp")
}

func containsMethod(methods []string, method string) bool {
	for _, item := range methods {
		if strings.EqualFold(strings.TrimSpace(item), method) {
			return true
		}
	}
	return false
}

func normalizeMethods(methods []string) []string {
	out := make([]string, 0, len(methods))
	seen := map[string]struct{}{}
	for _, raw := range methods {
		method := strings.ToLower(strings.TrimSpace(raw))
		if method == "" {
			continue
		}
		if _, ok := seen[method]; ok {
			continue
		}
		seen[method] = struct{}{}
		out = append(out, method)
	}
	if len(out) == 0 {
		out = []string{"passive"}
	}
	return out
}

func readARPNeighbors() map[string]string {
	out := map[string]string{}
	cmd := exec.Command("arp", "-a")
	raw, err := cmd.Output()
	if err != nil {
		return out
	}
	lines := strings.Split(string(raw), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		ip := ipv4Pattern.FindString(line)
		mac := macPattern.FindString(line)
		if ip == "" || mac == "" {
			continue
		}
		if parsed := net.ParseIP(ip); parsed == nil || parsed.To4() == nil {
			continue
		}
		mac = strings.ToUpper(strings.ReplaceAll(mac, "-", ":"))
		out[ip] = mac
	}
	return out
}

func pingHost(ip string, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ms := timeout.Milliseconds()
	if ms < 200 {
		ms = 200
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "ping", "-n", "1", "-w", strconv.FormatInt(ms, 10), ip)
	} else {
		sec := int(timeout.Seconds())
		if sec < 1 {
			sec = 1
		}
		cmd = exec.CommandContext(ctx, "ping", "-c", "1", "-W", strconv.Itoa(sec), ip)
	}
	return cmd.Run() == nil
}

func tcpReachable(ip string, ports []int, timeout time.Duration) bool {
	for _, port := range ports {
		address := net.JoinHostPort(ip, strconv.Itoa(port))
		conn, err := net.DialTimeout("tcp", address, timeout)
		if err == nil {
			_ = conn.Close()
			return true
		}
	}
	return false
}

func compareIPString(a, b string) int {
	addrA, errA := netip.ParseAddr(a)
	addrB, errB := netip.ParseAddr(b)
	if errA == nil && errB == nil {
		return addrA.Compare(addrB)
	}
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func (e *Engine) persistResult(result ScanResult) error {
	resultRaw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	hostsMap := map[string]ObservedHost{}
	if len(result.Subnets) > 0 {
		existing := e.loadPreviousHosts()
		scopedSubnets := make([]ipam.Subnet, 0, len(result.Subnets))
		for _, cidr := range result.Subnets {
			scopedSubnets = append(scopedSubnets, ipam.Subnet{CIDR: cidr})
		}
		for ip, host := range existing {
			if !ipMatchesSubnets(ip, host.Subnet, scopedSubnets) {
				hostsMap[ip] = host
			}
		}
	}
	for _, host := range result.Hosts {
		hostsMap[host.IP] = host
	}
	hostsRaw, err := json.Marshal(hostsMap)
	if err != nil {
		return err
	}
	metaLastRaw, err := result.CompletedAt.UTC().MarshalText()
	if err != nil {
		return err
	}
	key := []byte(fmt.Sprintf("%020d\x1f%s", result.StartedAt.UnixNano(), result.ScanID))

	if err := e.store.Tx(func(tx *storage.Tx) error {
		tx.Put(treeDiscoveryScans, key, resultRaw)
		tx.Put(treeDiscoveryMeta, []byte(keyLatestHosts), hostsRaw)
		tx.Put(treeDiscoveryMeta, []byte(keyLatestScanID), []byte(result.ScanID))
		tx.Put(treeDiscoveryMeta, []byte(keyLastScanAt), metaLastRaw)
		return nil
	}); err != nil {
		return err
	}

	e.mu.Lock()
	e.lastScanAt = result.CompletedAt
	e.nextScanAt = time.Now().UTC().Add(e.interval)
	e.latestID = result.ScanID
	e.mu.Unlock()
	return nil
}

func (e *Engine) ListResults(_ context.Context, limit int) ([]ScanResult, error) {
	out := make([]ScanResult, 0, 64)
	err := e.store.Iterate(treeDiscoveryScans, nil, nil, func(_, value []byte) bool {
		var result ScanResult
		if json.Unmarshal(value, &result) == nil {
			out = append(out, result)
		}
		return true
	})
	if err != nil && err != storage.ErrNotFound {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (e *Engine) GetResult(_ context.Context, scanID string) (ScanResult, error) {
	results, err := e.ListResults(context.Background(), 0)
	if err != nil {
		return ScanResult{}, err
	}
	for _, result := range results {
		if result.ScanID == scanID {
			return result, nil
		}
	}
	return ScanResult{}, storage.ErrNotFound
}

func (e *Engine) LatestResult(_ context.Context) (ScanResult, error) {
	e.mu.RLock()
	latestID := e.latestID
	e.mu.RUnlock()
	if latestID != "" {
		return e.GetResult(context.Background(), latestID)
	}
	results, err := e.ListResults(context.Background(), 1)
	if err != nil {
		return ScanResult{}, err
	}
	if len(results) == 0 {
		return ScanResult{}, storage.ErrNotFound
	}
	return results[0], nil
}

func (e *Engine) LatestConflicts(ctx context.Context) ([]Conflict, error) {
	result, err := e.LatestResult(ctx)
	if err != nil {
		if err == storage.ErrNotFound {
			return []Conflict{}, nil
		}
		return nil, err
	}
	return result.Conflicts, nil
}

func (e *Engine) LatestRogueServers(ctx context.Context) ([]RogueServer, error) {
	e.mu.RLock()
	if len(e.latestRogue) > 0 {
		out := make([]RogueServer, len(e.latestRogue))
		copy(out, e.latestRogue)
		e.mu.RUnlock()
		return out, nil
	}
	e.mu.RUnlock()
	result, err := e.LatestResult(ctx)
	if err != nil {
		if err == storage.ErrNotFound {
			return []RogueServer{}, nil
		}
		return nil, err
	}
	return result.RogueServers, nil
}

func (e *Engine) loadPreviousHosts() map[string]ObservedHost {
	raw, err := e.store.Get(treeDiscoveryMeta, []byte(keyLatestHosts))
	if err != nil {
		return map[string]ObservedHost{}
	}
	var out map[string]ObservedHost
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]ObservedHost{}
	}
	return out
}

func (e *Engine) loadMeta() {
	lastRaw, err := e.store.Get(treeDiscoveryMeta, []byte(keyLastScanAt))
	if err == nil {
		var t time.Time
		if t.UnmarshalText(lastRaw) == nil {
			e.lastScanAt = t
		}
	}
	latestRaw, err := e.store.Get(treeDiscoveryMeta, []byte(keyLatestScanID))
	if err == nil {
		e.latestID = string(latestRaw)
	}
	rogueRaw, err := e.store.Get(treeDiscoveryMeta, []byte(keyLatestRogue))
	if err == nil {
		var rogue []RogueServer
		if json.Unmarshal(rogueRaw, &rogue) == nil {
			e.latestRogue = rogue
		}
	}
	e.nextScanAt = time.Now().UTC().Add(e.interval)
}

func (e *Engine) RecordRogueServer(_ context.Context, item RogueServer) error {
	if strings.TrimSpace(item.IP) == "" {
		return nil
	}
	item.IP = strings.TrimSpace(item.IP)
	item.MAC = strings.ToUpper(strings.TrimSpace(item.MAC))
	if item.Vendor == "" {
		item.Vendor = LookupVendor(item.MAC)
	}
	if item.Detected.IsZero() {
		item.Detected = time.Now().UTC()
	}

	e.mu.Lock()
	merged := make([]RogueServer, 0, len(e.latestRogue)+1)
	merged = append(merged, item)
	for _, existing := range e.latestRogue {
		if strings.EqualFold(existing.IP, item.IP) {
			continue
		}
		merged = append(merged, existing)
		if len(merged) >= 16 {
			break
		}
	}
	e.latestRogue = merged
	snapshot := make([]RogueServer, len(e.latestRogue))
	copy(snapshot, e.latestRogue)
	e.mu.Unlock()

	raw, err := encodeRogueServers(snapshot)
	if err != nil {
		return err
	}
	return e.store.Tx(func(tx *storage.Tx) error {
		tx.Put(treeDiscoveryMeta, []byte(keyLatestRogue), raw)
		return nil
	})
}

func normalizeReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "manual"
	}
	return reason
}
