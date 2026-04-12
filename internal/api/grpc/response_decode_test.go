package grpc

func decodeSystemHealthResponseTest(data []byte) (systemHealthResponse, error) {
	var out systemHealthResponse
	err := readProtoFields(data, func(field int, wireType int, raw []byte, value uint64) error {
		switch field {
		case 1:
			out.Status = string(raw)
		case 2:
			out.Ready = parseBool(value)
		case 3:
			out.Version = string(raw)
		case 4:
			out.Uptime = string(raw)
		case 5:
			out.PayloadJSON = string(raw)
		}
		return nil
	})
	return out, err
}

func decodeSubnetMessageTest(data []byte) (subnetMessage, error) {
	var out subnetMessage
	err := readProtoFields(data, func(field int, wireType int, raw []byte, value uint64) error {
		switch field {
		case 1:
			out.CIDR = string(raw)
		case 2:
			out.Name = string(raw)
		case 3:
			out.VLAN = int(value)
		case 4:
			out.Gateway = string(raw)
		case 5:
			out.DNS = append(out.DNS, string(raw))
		case 6:
			out.DHCP = parseBool(value)
		case 7:
			out.PoolStart = string(raw)
		case 8:
			out.PoolEnd = string(raw)
		case 9:
			out.LeaseSec = int64(value)
		case 10:
			out.CreatedAt = int64(value)
		case 11:
			out.UpdatedAt = int64(value)
		}
		return nil
	})
	return out, err
}

func decodeLeaseMessageTest(data []byte) (leaseMessage, error) {
	var out leaseMessage
	err := readProtoFields(data, func(field int, wireType int, raw []byte, value uint64) error {
		switch field {
		case 1:
			out.IP = string(raw)
		case 2:
			out.MAC = string(raw)
		case 3:
			out.Hostname = string(raw)
		case 4:
			out.State = string(raw)
		case 5:
			out.SubnetID = string(raw)
		case 6:
			out.RelayAddr = string(raw)
		case 7:
			out.ExpiryUnix = int64(value)
		case 8:
			out.UpdatedAt = int64(value)
		case 9:
			out.Duration = int64(value)
		}
		return nil
	})
	return out, err
}

func decodeLeaseEventMessageTest(data []byte) (leaseEventMessage, error) {
	var out leaseEventMessage
	err := readProtoFields(data, func(field int, wireType int, raw []byte, value uint64) error {
		switch field {
		case 1:
			out.Type = string(raw)
		case 2:
			out.IP = string(raw)
		case 3:
			item, err := decodeLeaseMessageTest(raw)
			if err != nil {
				return err
			}
			out.Lease = &item
		case 4:
			out.OccurredAt = int64(value)
		}
		return nil
	})
	return out, err
}

func decodeIPAddressMessageTest(data []byte) (ipAddressMessage, error) {
	var out ipAddressMessage
	err := readProtoFields(data, func(field int, wireType int, raw []byte, value uint64) error {
		switch field {
		case 1:
			out.IP = string(raw)
		case 2:
			out.SubnetCIDR = string(raw)
		case 3:
			out.State = string(raw)
		case 4:
			out.MAC = string(raw)
		case 5:
			out.Hostname = string(raw)
		case 6:
			out.LeaseState = string(raw)
		case 7:
			out.Source = string(raw)
		case 8:
			out.UpdatedAt = int64(value)
		}
		return nil
	})
	return out, err
}

func decodeDiscoveryEventMessageTest(data []byte) (discoveryEventMessage, error) {
	var out discoveryEventMessage
	err := readProtoFields(data, func(field int, wireType int, raw []byte, value uint64) error {
		switch field {
		case 1:
			out.Type = string(raw)
		case 2:
			out.ScanID = string(raw)
		case 3:
			out.Subnet = string(raw)
		case 4:
			out.IP = string(raw)
		case 5:
			out.Found = int(value)
		case 6:
			out.MACs = append(out.MACs, string(raw))
		case 7:
			out.OccurredAt = int64(value)
		case 8:
			out.Note = string(raw)
		}
		return nil
	})
	return out, err
}
