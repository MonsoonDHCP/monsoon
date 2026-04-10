package mcp

type ToolDefinition struct {
	Name         string         `json:"name"`
	Title        string         `json:"title,omitempty"`
	Description  string         `json:"description"`
	InputSchema  map[string]any `json:"inputSchema"`
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
	Annotations  map[string]any `json:"annotations,omitempty"`
}

func DefaultTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "monsoon_list_subnets",
			Title:       "List Subnets",
			Description: "List all managed subnets with utilization and DHCP status for planning or inventory workflows.",
			InputSchema: schemaObject(nil, nil),
			Annotations: readOnlyTool(),
		},
		{
			Name:        "monsoon_get_subnet",
			Title:       "Get Subnet",
			Description: "Return full details for a specific subnet CIDR, including DHCP pool settings and utilization snapshot.",
			InputSchema: schemaObject(map[string]any{
				"cidr": schemaString("Subnet CIDR to inspect, for example 10.0.10.0/24."),
			}, []string{"cidr"}),
			Annotations: readOnlyTool(),
		},
		{
			Name:        "monsoon_create_subnet",
			Title:       "Create Subnet",
			Description: "Create or update a subnet definition in Monsoon, including gateway, DNS, VLAN, and DHCP pool bounds.",
			InputSchema: schemaObject(map[string]any{
				"cidr":           schemaString("Subnet CIDR to create or update."),
				"name":           schemaString("Human-friendly subnet name."),
				"vlan":           schemaInteger("Optional VLAN ID between 0 and 4094."),
				"gateway":        schemaString("Optional gateway IP inside the subnet."),
				"dns":            schemaStringArray("Optional DNS server addresses for the subnet."),
				"dhcp_enabled":   schemaBoolean("Whether DHCP allocation is enabled for this subnet."),
				"pool_start":     schemaString("DHCP pool start IP when DHCP is enabled."),
				"pool_end":       schemaString("DHCP pool end IP when DHCP is enabled."),
				"lease_time_sec": schemaInteger("Lease duration in seconds when DHCP is enabled."),
			}, []string{"cidr"}),
		},
		{
			Name:        "monsoon_find_available_ip",
			Title:       "Find Available IP",
			Description: "Find the next available address inside a subnet's DHCP pool by checking leases, reservations, and stored address state.",
			InputSchema: schemaObject(map[string]any{
				"subnet_cidr": schemaString("Target subnet CIDR."),
			}, []string{"subnet_cidr"}),
			Annotations: readOnlyTool(),
		},
		{
			Name:        "monsoon_reserve_ip",
			Title:       "Reserve IP",
			Description: "Create or update a DHCP reservation for a MAC address and fixed IP inside a managed subnet.",
			InputSchema: schemaObject(map[string]any{
				"mac":         schemaString("Client MAC address, for example AA:BB:CC:DD:EE:FF."),
				"ip":          schemaString("Reserved IPv4 address."),
				"hostname":    schemaString("Optional hostname associated with the reservation."),
				"subnet_cidr": schemaString("Optional subnet CIDR if you want to force the reservation into a specific subnet."),
			}, []string{"mac", "ip"}),
		},
		{
			Name:        "monsoon_list_leases",
			Title:       "List Leases",
			Description: "List DHCP leases with optional subnet and state filtering. By default, only active leases are returned.",
			InputSchema: schemaObject(map[string]any{
				"subnet_cidr":      schemaString("Optional subnet CIDR to filter by."),
				"state":            schemaString("Optional exact lease state filter."),
				"include_inactive": schemaBoolean("Include released, expired, or declined leases."),
			}, nil),
			Annotations: readOnlyTool(),
		},
		{
			Name:        "monsoon_get_lease",
			Title:       "Get Lease",
			Description: "Get full lease details for a specific IP address.",
			InputSchema: schemaObject(map[string]any{
				"ip": schemaString("Lease IP address."),
			}, []string{"ip"}),
			Annotations: readOnlyTool(),
		},
		{
			Name:        "monsoon_search_by_mac",
			Title:       "Search By MAC",
			Description: "Find all known reservations, leases, and address records associated with a MAC address.",
			InputSchema: schemaObject(map[string]any{
				"mac": schemaString("MAC address to search for."),
			}, []string{"mac"}),
			Annotations: readOnlyTool(),
		},
		{
			Name:        "monsoon_search_by_hostname",
			Title:       "Search By Hostname",
			Description: "Search leases, reservations, and address records by hostname or hostname fragment.",
			InputSchema: schemaObject(map[string]any{
				"hostname": schemaString("Hostname or partial hostname to search for."),
			}, []string{"hostname"}),
			Annotations: readOnlyTool(),
		},
		{
			Name:        "monsoon_subnet_utilization",
			Title:       "Subnet Utilization",
			Description: "Return subnet capacity, used addresses, available addresses, and state breakdown for a subnet or for all managed subnets.",
			InputSchema: schemaObject(map[string]any{
				"subnet_cidr": schemaString("Optional subnet CIDR. When omitted, utilization is returned for all managed subnets."),
			}, nil),
			Annotations: readOnlyTool(),
		},
		{
			Name:        "monsoon_run_discovery",
			Title:       "Run Discovery",
			Description: "Trigger a discovery scan to compare live network observations with DHCP and IPAM state.",
			InputSchema: schemaObject(map[string]any{
				"reason":  schemaString("Optional reason string that will be attached to the scan."),
				"subnets": schemaStringArray("Optional list of subnet CIDRs to target."),
			}, nil),
		},
		{
			Name:        "monsoon_get_conflicts",
			Title:       "Get Conflicts",
			Description: "Return the latest detected IP conflicts from the discovery subsystem.",
			InputSchema: schemaObject(nil, nil),
			Annotations: readOnlyTool(),
		},
		{
			Name:        "monsoon_audit_query",
			Title:       "Audit Query",
			Description: "Search Monsoon audit entries by actor, action, object, free-text query, time range, and limit.",
			InputSchema: schemaObject(map[string]any{
				"actor":       schemaString("Optional actor username."),
				"action":      schemaString("Optional action name."),
				"object_type": schemaString("Optional object type."),
				"object_id":   schemaString("Optional object identifier."),
				"source":      schemaString("Optional source value such as api or mcp."),
				"q":           schemaString("Optional free-text query."),
				"from":        schemaString("Optional RFC3339 lower bound."),
				"to":          schemaString("Optional RFC3339 upper bound."),
				"limit":       schemaInteger("Maximum number of entries to return. Defaults to 50."),
			}, nil),
			Annotations: readOnlyTool(),
		},
		{
			Name:        "monsoon_get_health",
			Title:       "Get Health",
			Description: "Return current Monsoon service health, uptime, DHCP listener state, and MCP listener metadata.",
			InputSchema: schemaObject(nil, nil),
			Annotations: readOnlyTool(),
		},
		{
			Name:        "monsoon_plan_subnet",
			Title:       "Plan Subnet",
			Description: "Generate a subnet sizing suggestion for a workload, including CIDR, usable host count, gateway, and DHCP pool recommendation.",
			InputSchema: schemaObject(map[string]any{
				"prompt":             schemaString("Optional free-form planning request, for example 'I need 500 addresses for IoT devices'."),
				"required_addresses": schemaInteger("Required usable addresses before growth headroom."),
				"growth_percent":     schemaInteger("Optional growth buffer percentage. Defaults to 15."),
				"parent_cidr":        schemaString("Optional parent CIDR to place the subnet inside. Defaults to 10.0.0.0/8."),
				"name":               schemaString("Optional suggested subnet name."),
				"vlan":               schemaInteger("Optional VLAN hint."),
			}, nil),
			Annotations: readOnlyTool(),
		},
	}
}

func schemaObject(properties map[string]any, required []string) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	out := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

func schemaString(description string) map[string]any {
	return map[string]any{
		"type":        "string",
		"description": description,
	}
}

func schemaInteger(description string) map[string]any {
	return map[string]any{
		"type":        "integer",
		"description": description,
	}
}

func schemaBoolean(description string) map[string]any {
	return map[string]any{
		"type":        "boolean",
		"description": description,
	}
}

func schemaStringArray(description string) map[string]any {
	return map[string]any{
		"type":        "array",
		"description": description,
		"items": map[string]any{
			"type": "string",
		},
	}
}

func readOnlyTool() map[string]any {
	return map[string]any{
		"readOnlyHint": true,
	}
}
