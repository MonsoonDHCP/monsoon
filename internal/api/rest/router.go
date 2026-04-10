package rest

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monsoondhcp/monsoon/internal/audit"
	"github.com/monsoondhcp/monsoon/internal/auth"
	"github.com/monsoondhcp/monsoon/internal/discovery"
	"github.com/monsoondhcp/monsoon/internal/events"
	"github.com/monsoondhcp/monsoon/internal/ipam"
	"github.com/monsoondhcp/monsoon/internal/lease"
	uisettings "github.com/monsoondhcp/monsoon/internal/settings"
)

type DashboardConfig struct {
	Enabled  bool
	DistDir  string
	BasePath string
}

type RouterDeps struct {
	LeaseStore       lease.Store
	IPAMEngine       *ipam.Engine
	DiscoveryEngine  *discovery.Engine
	AuthService      *auth.Service
	AuthSecureCookie bool
	AuditLogger      *audit.Logger
	Version          string
	MetricsPath      string
	DHCPv4Enabled    bool
	DHCPv4Listen     string
	DHCPv4Running    func() bool
	Dashboard        DashboardConfig
	UISettings       uisettings.UIStore
	EventBroker      *events.Broker
}

func RegisterRoutes(mux *http.ServeMux, deps RouterDeps) error {
	mux.HandleFunc("GET /api/v1/system/health", func(w http.ResponseWriter, _ *http.Request) {
		running := false
		if deps.DHCPv4Running != nil {
			running = deps.DHCPv4Running()
		}
		WriteJSON(w, http.StatusOK, map[string]any{
			"status":  "healthy",
			"version": deps.Version,
			"components": map[string]any{
				"dhcpv4": map[string]any{
					"enabled": deps.DHCPv4Enabled,
					"listen":  deps.DHCPv4Listen,
					"running": running,
				},
			},
		}, nil)
	})

	if deps.LeaseStore != nil {
		registerLeaseRoutes(mux, deps.LeaseStore, deps.IPAMEngine, deps.EventBroker, deps.AuditLogger)
	}
	if deps.DiscoveryEngine != nil {
		registerDiscoveryRoutes(mux, deps.DiscoveryEngine, deps.EventBroker, deps.AuditLogger)
	}
	if deps.AuthService != nil {
		registerAuthRoutes(mux, deps.AuthService, deps.AuthSecureCookie, deps.AuditLogger)
	}
	if deps.IPAMEngine != nil {
		registerSubnetRoutes(mux, deps.IPAMEngine, deps.EventBroker, deps.AuditLogger)
		registerReservationRoutes(mux, deps.IPAMEngine, deps.EventBroker, deps.AuditLogger)
		registerAddressRoutes(mux, deps.IPAMEngine)
	}
	if deps.AuditLogger != nil {
		registerAuditRoutes(mux, deps.AuditLogger)
	}
	if deps.UISettings != nil {
		registerSettingsRoutes(mux, deps.UISettings, deps.EventBroker, deps.AuditLogger)
	}
	if deps.EventBroker != nil {
		registerEventRoutes(mux, deps.EventBroker)
	}

	if deps.Dashboard.Enabled {
		h, err := NewSPADashboardHandler(deps.Dashboard.DistDir, deps.Dashboard.BasePath, deps.MetricsPath)
		if err != nil {
			return err
		}
		mux.Handle("GET /{$}", h)
		mux.Handle("GET /{path...}", h)
	} else {
		mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
			WriteJSON(w, http.StatusOK, map[string]string{"name": "Monsoon", "status": "running"}, nil)
		})
	}

	return nil
}

func registerLeaseRoutes(mux *http.ServeMux, store lease.Store, engine *ipam.Engine, broker *events.Broker, logger *audit.Logger) {
	mux.HandleFunc("GET /api/v1/leases", func(w http.ResponseWriter, r *http.Request) {
		all, err := store.ListAll(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "lease_list_failed", err.Error())
			return
		}
		state := r.URL.Query().Get("state")
		subnet := r.URL.Query().Get("subnet")
		filtered := make([]lease.Lease, 0, len(all))
		for _, l := range all {
			if state != "" && string(l.State) != state {
				continue
			}
			if subnet != "" && l.SubnetID != subnet {
				continue
			}
			filtered = append(filtered, l)
		}
		sort.Slice(filtered, func(i, j int) bool {
			return filtered[i].IP < filtered[j].IP
		})
		WriteJSON(w, http.StatusOK, filtered, map[string]any{"total": len(filtered)})
	})

	mux.HandleFunc("GET /api/v1/leases/{ip}", func(w http.ResponseWriter, r *http.Request) {
		ip := r.PathValue("ip")
		l, err := store.GetByIP(r.Context(), ip)
		if err != nil {
			WriteError(w, http.StatusNotFound, "lease_not_found", "lease not found")
			return
		}
		WriteJSON(w, http.StatusOK, l, nil)
	})

	mux.HandleFunc("POST /api/v1/leases/{ip}/release", func(w http.ResponseWriter, r *http.Request) {
		if !requireRoleForMutation(w, r, auth.DefaultRoleOperator) {
			return
		}
		ip := r.PathValue("ip")
		l, err := store.GetByIP(r.Context(), ip)
		if err != nil {
			WriteError(w, http.StatusNotFound, "lease_not_found", "lease not found")
			return
		}
		now := time.Now().UTC()
		l.State = lease.StateReleased
		l.ExpiryTime = now
		l.UpdatedAt = now
		if err := store.Upsert(context.Background(), l); err != nil {
			WriteError(w, http.StatusInternalServerError, "lease_release_failed", err.Error())
			return
		}
		if broker != nil {
			broker.Publish(events.Event{Type: "lease.released", Data: map[string]any{"ip": ip}})
		}
		logAuditEntry(r, logger, audit.Entry{
			Actor:      requestActor(r),
			Action:     "lease.release",
			ObjectType: "lease",
			ObjectID:   ip,
			Source:     "api",
			After: map[string]any{
				"state": "released",
			},
		})
		WriteJSON(w, http.StatusOK, map[string]string{"status": "released", "ip": ip}, nil)
	})

	if engine != nil {
		mux.HandleFunc("POST /api/v1/leases/{ip}/reservation", func(w http.ResponseWriter, r *http.Request) {
			if !requireRoleForMutation(w, r, auth.DefaultRoleOperator) {
				return
			}
			ip := r.PathValue("ip")
			l, err := store.GetByIP(r.Context(), ip)
			if err != nil {
				WriteError(w, http.StatusNotFound, "lease_not_found", "lease not found")
				return
			}
			item, err := engine.UpsertReservation(r.Context(), ipam.UpsertReservationInput{
				MAC:        l.MAC,
				IP:         l.IP,
				Hostname:   l.Hostname,
				SubnetCIDR: l.SubnetID,
			})
			if err != nil {
				WriteError(w, http.StatusBadRequest, "reservation_create_failed", err.Error())
				return
			}
			if broker != nil {
				broker.Publish(events.Event{Type: "reservation.upserted", Data: map[string]any{"mac": item.MAC, "ip": item.IP}})
			}
			logAuditEntry(r, logger, audit.Entry{
				Actor:      requestActor(r),
				Action:     "reservation.create_from_lease",
				ObjectType: "reservation",
				ObjectID:   item.MAC,
				Source:     "api",
				After: map[string]any{
					"ip":     item.IP,
					"mac":    item.MAC,
					"subnet": item.SubnetCIDR,
				},
			})
			WriteJSON(w, http.StatusOK, item, nil)
		})
	}
}

func registerSubnetRoutes(mux *http.ServeMux, engine *ipam.Engine, broker *events.Broker, logger *audit.Logger) {
	mux.HandleFunc("GET /api/v1/subnets", func(w http.ResponseWriter, r *http.Request) {
		subnets, err := engine.ListSummaries(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "subnet_list_failed", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, subnets, map[string]any{"total": len(subnets)})
	})

	mux.HandleFunc("GET /api/v1/subnets/raw", func(w http.ResponseWriter, r *http.Request) {
		subnets, err := engine.ListSubnets(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "subnet_list_failed", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, subnets, map[string]any{"total": len(subnets)})
	})

	upsert := func(w http.ResponseWriter, r *http.Request) {
		if !requireRoleForMutation(w, r, auth.DefaultRoleOperator) {
			return
		}
		var payload ipam.UpsertSubnetInput
		if err := decodeJSONBody(r, &payload); err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_payload", err.Error())
			return
		}
		result, err := engine.UpsertSubnet(r.Context(), payload)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "subnet_upsert_failed", err.Error())
			return
		}
		if broker != nil {
			broker.Publish(events.Event{Type: "subnet.upserted", Data: map[string]any{"cidr": result.CIDR}})
		}
		logAuditEntry(r, logger, audit.Entry{
			Actor:      requestActor(r),
			Action:     "subnet.upsert",
			ObjectType: "subnet",
			ObjectID:   result.CIDR,
			Source:     "api",
			After: map[string]any{
				"name":         result.Name,
				"vlan":         result.VLAN,
				"gateway":      result.Gateway,
				"dhcp_enabled": result.DHCP.Enabled,
			},
		})
		WriteJSON(w, http.StatusOK, result, nil)
	}

	mux.HandleFunc("POST /api/v1/subnets", upsert)
	mux.HandleFunc("PUT /api/v1/subnets", upsert)

	mux.HandleFunc("DELETE /api/v1/subnets", func(w http.ResponseWriter, r *http.Request) {
		if !requireRoleForMutation(w, r, auth.DefaultRoleOperator) {
			return
		}
		cidr := strings.TrimSpace(r.URL.Query().Get("cidr"))
		if cidr == "" {
			WriteError(w, http.StatusBadRequest, "missing_cidr", "query parameter cidr is required")
			return
		}
		if err := engine.DeleteSubnet(r.Context(), cidr); err != nil {
			WriteError(w, http.StatusInternalServerError, "subnet_delete_failed", err.Error())
			return
		}
		if broker != nil {
			broker.Publish(events.Event{Type: "subnet.deleted", Data: map[string]any{"cidr": cidr}})
		}
		logAuditEntry(r, logger, audit.Entry{
			Actor:      requestActor(r),
			Action:     "subnet.delete",
			ObjectType: "subnet",
			ObjectID:   cidr,
			Source:     "api",
		})
		WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted", "cidr": cidr}, nil)
	})
}

func registerReservationRoutes(mux *http.ServeMux, engine *ipam.Engine, broker *events.Broker, logger *audit.Logger) {
	mux.HandleFunc("GET /api/v1/reservations", func(w http.ResponseWriter, r *http.Request) {
		records, err := engine.ListReservations(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "reservation_list_failed", err.Error())
			return
		}
		q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
		subnet := strings.TrimSpace(r.URL.Query().Get("subnet"))
		mac := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("mac")))

		filtered := make([]ipam.Reservation, 0, len(records))
		for _, item := range records {
			if subnet != "" && item.SubnetCIDR != subnet {
				continue
			}
			if mac != "" && item.MAC != mac {
				continue
			}
			if q != "" {
				h := strings.ToLower(strings.Join([]string{item.IP, item.MAC, item.Hostname, item.SubnetCIDR}, " "))
				if !strings.Contains(h, q) {
					continue
				}
			}
			filtered = append(filtered, item)
		}
		WriteJSON(w, http.StatusOK, filtered, map[string]any{"total": len(filtered)})
	})

	mux.HandleFunc("GET /api/v1/reservations/{mac}", func(w http.ResponseWriter, r *http.Request) {
		mac := strings.TrimSpace(r.PathValue("mac"))
		item, err := engine.GetReservationByMAC(r.Context(), mac)
		if err != nil {
			WriteError(w, http.StatusNotFound, "reservation_not_found", "reservation not found")
			return
		}
		WriteJSON(w, http.StatusOK, item, nil)
	})

	upsert := func(w http.ResponseWriter, r *http.Request) {
		if !requireRoleForMutation(w, r, auth.DefaultRoleOperator) {
			return
		}
		var payload ipam.UpsertReservationInput
		if err := decodeJSONBody(r, &payload); err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_payload", err.Error())
			return
		}
		item, err := engine.UpsertReservation(r.Context(), payload)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "reservation_upsert_failed", err.Error())
			return
		}
		if broker != nil {
			broker.Publish(events.Event{Type: "reservation.upserted", Data: map[string]any{"mac": item.MAC, "ip": item.IP}})
		}
		logAuditEntry(r, logger, audit.Entry{
			Actor:      requestActor(r),
			Action:     "reservation.upsert",
			ObjectType: "reservation",
			ObjectID:   item.MAC,
			Source:     "api",
			After: map[string]any{
				"ip":     item.IP,
				"subnet": item.SubnetCIDR,
			},
		})
		WriteJSON(w, http.StatusOK, item, nil)
	}

	mux.HandleFunc("POST /api/v1/reservations", upsert)
	mux.HandleFunc("PUT /api/v1/reservations", upsert)

	mux.HandleFunc("DELETE /api/v1/reservations", func(w http.ResponseWriter, r *http.Request) {
		if !requireRoleForMutation(w, r, auth.DefaultRoleOperator) {
			return
		}
		mac := strings.TrimSpace(r.URL.Query().Get("mac"))
		if mac == "" {
			WriteError(w, http.StatusBadRequest, "missing_mac", "query parameter mac is required")
			return
		}
		if err := engine.DeleteReservation(r.Context(), mac); err != nil {
			WriteError(w, http.StatusInternalServerError, "reservation_delete_failed", err.Error())
			return
		}
		if broker != nil {
			broker.Publish(events.Event{Type: "reservation.deleted", Data: map[string]any{"mac": strings.ToUpper(mac)}})
		}
		logAuditEntry(r, logger, audit.Entry{
			Actor:      requestActor(r),
			Action:     "reservation.delete",
			ObjectType: "reservation",
			ObjectID:   strings.ToUpper(mac),
			Source:     "api",
		})
		WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted", "mac": strings.ToUpper(mac)}, nil)
	})
}

func registerAddressRoutes(mux *http.ServeMux, engine *ipam.Engine) {
	mux.HandleFunc("GET /api/v1/addresses", func(w http.ResponseWriter, r *http.Request) {
		limit := 0
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 0 {
				WriteError(w, http.StatusBadRequest, "invalid_limit", "limit must be a positive integer")
				return
			}
			limit = parsed
		}

		filter := ipam.AddressFilter{
			SubnetCIDR: strings.TrimSpace(r.URL.Query().Get("subnet")),
			State:      ipam.IPState(strings.TrimSpace(r.URL.Query().Get("state"))),
			Query:      strings.TrimSpace(r.URL.Query().Get("q")),
			Limit:      limit,
		}
		records, err := engine.ListAddresses(r.Context(), filter)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "address_list_failed", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, records, map[string]any{"total": len(records)})
	})

	mux.HandleFunc("GET /api/v1/addresses/{ip}", func(w http.ResponseWriter, r *http.Request) {
		ip := r.PathValue("ip")
		record, err := engine.GetAddress(r.Context(), ip)
		if err != nil {
			WriteError(w, http.StatusNotFound, "address_not_found", "address not found")
			return
		}
		WriteJSON(w, http.StatusOK, record, nil)
	})
}

func registerAuditRoutes(mux *http.ServeMux, logger *audit.Logger) {
	mux.HandleFunc("GET /api/v1/audit", func(w http.ResponseWriter, r *http.Request) {
		limit := 100
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed <= 0 {
				WriteError(w, http.StatusBadRequest, "invalid_limit", "limit must be positive")
				return
			}
			limit = parsed
		}

		var from, to time.Time
		var err error
		if raw := strings.TrimSpace(r.URL.Query().Get("from")); raw != "" {
			from, err = time.Parse(time.RFC3339, raw)
			if err != nil {
				WriteError(w, http.StatusBadRequest, "invalid_from", "from must be RFC3339")
				return
			}
		}
		if raw := strings.TrimSpace(r.URL.Query().Get("to")); raw != "" {
			to, err = time.Parse(time.RFC3339, raw)
			if err != nil {
				WriteError(w, http.StatusBadRequest, "invalid_to", "to must be RFC3339")
				return
			}
		}

		entries, err := logger.Query(r.Context(), audit.QueryFilter{
			Actor:      strings.TrimSpace(r.URL.Query().Get("actor")),
			Action:     strings.TrimSpace(r.URL.Query().Get("action")),
			ObjectType: strings.TrimSpace(r.URL.Query().Get("object_type")),
			ObjectID:   strings.TrimSpace(r.URL.Query().Get("object_id")),
			Source:     strings.TrimSpace(r.URL.Query().Get("source")),
			Query:      strings.TrimSpace(r.URL.Query().Get("q")),
			From:       from,
			To:         to,
			Limit:      limit,
		})
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "audit_query_failed", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, entries, map[string]any{"total": len(entries)})
	})
}

func registerDiscoveryRoutes(mux *http.ServeMux, engine *discovery.Engine, broker *events.Broker, logger *audit.Logger) {
	mux.HandleFunc("GET /api/v1/discovery/status", func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, http.StatusOK, engine.Status(r.Context()), nil)
	})

	mux.HandleFunc("POST /api/v1/discovery/scan", func(w http.ResponseWriter, r *http.Request) {
		if !requireRoleForMutation(w, r, auth.DefaultRoleOperator) {
			return
		}
		var payload discovery.ScanRequest
		if r.ContentLength > 0 {
			if err := decodeJSONBody(r, &payload); err != nil {
				WriteError(w, http.StatusBadRequest, "invalid_payload", err.Error())
				return
			}
		}
		scanID, err := engine.TriggerScan(r.Context(), payload)
		if err != nil {
			WriteError(w, http.StatusConflict, "scan_in_progress", err.Error())
			return
		}
		if broker != nil {
			broker.Publish(events.Event{Type: "discovery.scan_queued", Data: map[string]any{"scan_id": scanID}})
		}
		logAuditEntry(r, logger, audit.Entry{
			Actor:      requestActor(r),
			Action:     "discovery.scan.trigger",
			ObjectType: "discovery_scan",
			ObjectID:   scanID,
			Source:     "api",
			After: map[string]any{
				"reason": payload.Reason,
			},
		})
		WriteJSON(w, http.StatusAccepted, map[string]any{
			"status":       "queued",
			"scan_id":      scanID,
			"estimated_in": "15s",
		}, nil)
	})

	mux.HandleFunc("GET /api/v1/discovery/results", func(w http.ResponseWriter, r *http.Request) {
		limit := 50
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed <= 0 {
				WriteError(w, http.StatusBadRequest, "invalid_limit", "limit must be positive")
				return
			}
			limit = parsed
		}
		results, err := engine.ListResults(r.Context(), limit)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "discovery_results_failed", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, results, map[string]any{"total": len(results)})
	})

	mux.HandleFunc("GET /api/v1/discovery/results/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.PathValue("id"))
		result, err := engine.GetResult(r.Context(), id)
		if err != nil {
			WriteError(w, http.StatusNotFound, "discovery_result_not_found", "scan result not found")
			return
		}
		WriteJSON(w, http.StatusOK, result, nil)
	})

	mux.HandleFunc("GET /api/v1/discovery/conflicts", func(w http.ResponseWriter, r *http.Request) {
		conflicts, err := engine.LatestConflicts(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "discovery_conflicts_failed", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, conflicts, map[string]any{"total": len(conflicts)})
	})

	mux.HandleFunc("GET /api/v1/discovery/rogue", func(w http.ResponseWriter, r *http.Request) {
		rogue, err := engine.LatestRogueServers(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "discovery_rogue_failed", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, rogue, map[string]any{"total": len(rogue)})
	})
}

func registerSettingsRoutes(mux *http.ServeMux, settings uisettings.UIStore, broker *events.Broker, logger *audit.Logger) {
	mux.HandleFunc("GET /api/v1/settings/ui", func(w http.ResponseWriter, _ *http.Request) {
		current, err := settings.Get(context.Background())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "settings_read_failed", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, current, nil)
	})

	mux.HandleFunc("PUT /api/v1/settings/ui", func(w http.ResponseWriter, r *http.Request) {
		if !requireRoleForMutation(w, r, auth.DefaultRoleOperator) {
			return
		}
		var payload uisettings.UISettings
		if err := decodeJSONBody(r, &payload); err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_payload", err.Error())
			return
		}
		if err := settings.Set(r.Context(), payload); err != nil {
			WriteError(w, http.StatusInternalServerError, "settings_write_failed", err.Error())
			return
		}
		if broker != nil {
			broker.Publish(events.Event{Type: "settings.ui_updated", Data: map[string]any{"theme": payload.Theme, "density": payload.Density}})
		}
		logAuditEntry(r, logger, audit.Entry{
			Actor:      requestActor(r),
			Action:     "settings.ui.update",
			ObjectType: "settings",
			ObjectID:   "ui",
			Source:     "api",
			After: map[string]any{
				"theme":        payload.Theme,
				"density":      payload.Density,
				"auto_refresh": payload.AutoRefresh,
			},
		})
		current, err := settings.Get(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "settings_read_failed", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, current, nil)
	})
}

func decodeJSONBody(r *http.Request, out any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func logAuditEntry(r *http.Request, logger *audit.Logger, entry audit.Entry) {
	if logger == nil {
		return
	}
	_ = logger.Log(r.Context(), entry)
}

func requestActor(r *http.Request) string {
	if identity, ok := IdentityFromContext(r.Context()); ok {
		return identity.Username
	}
	return "anonymous"
}

func requireRoleForMutation(w http.ResponseWriter, r *http.Request, requiredRole string) bool {
	identity, ok := IdentityFromContext(r.Context())
	if !ok {
		return true
	}
	if !auth.HasRole(requiredRole, identity.Role) {
		WriteError(w, http.StatusForbidden, "forbidden", "insufficient role")
		return false
	}
	return true
}

func registerEventRoutes(mux *http.ServeMux, broker *events.Broker) {
	mux.HandleFunc("GET /api/v1/events", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			WriteError(w, http.StatusInternalServerError, "sse_not_supported", "streaming unsupported")
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		_, ch, unsubscribe := broker.Subscribe()
		defer unsubscribe()

		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()

		writeEvent := func(evt events.Event) error {
			payload, err := json.Marshal(evt)
			if err != nil {
				return err
			}
			if _, err := w.Write([]byte("event: " + evt.Type + "\n")); err != nil {
				return err
			}
			if _, err := w.Write([]byte("data: " + string(payload) + "\n\n")); err != nil {
				return err
			}
			flusher.Flush()
			return nil
		}

		if err := writeEvent(events.Event{Type: "system.connected", Data: map[string]any{"ok": true}}); err != nil {
			return
		}

		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				if err := writeEvent(events.Event{Type: "system.keepalive"}); err != nil {
					return
				}
			case evt, ok := <-ch:
				if !ok {
					return
				}
				if err := writeEvent(evt); err != nil {
					return
				}
			}
		}
	})
}
