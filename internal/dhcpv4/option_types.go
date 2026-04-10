package dhcpv4

type OptionValueType string

const (
	TypeIPv4      OptionValueType = "ipv4"
	TypeIPv4List  OptionValueType = "ipv4_list"
	TypeUint8     OptionValueType = "uint8"
	TypeUint16    OptionValueType = "uint16"
	TypeUint32    OptionValueType = "uint32"
	TypeString    OptionValueType = "string"
	TypeBinary    OptionValueType = "binary"
	TypeIPPair    OptionValueType = "ip_pair"
	TypeRouteList OptionValueType = "route_list"
)

type OptionSpec struct {
	Code int
	Name string
	Type OptionValueType
}

var OptionSpecs = map[byte]OptionSpec{
	1:   {Code: 1, Name: "subnet-mask", Type: TypeIPv4},
	3:   {Code: 3, Name: "router", Type: TypeIPv4List},
	6:   {Code: 6, Name: "domain-name-server", Type: TypeIPv4List},
	12:  {Code: 12, Name: "host-name", Type: TypeString},
	15:  {Code: 15, Name: "domain-name", Type: TypeString},
	42:  {Code: 42, Name: "ntp-servers", Type: TypeIPv4List},
	50:  {Code: 50, Name: "requested-ip-address", Type: TypeIPv4},
	51:  {Code: 51, Name: "ip-address-lease-time", Type: TypeUint32},
	53:  {Code: 53, Name: "dhcp-message-type", Type: TypeUint8},
	54:  {Code: 54, Name: "server-identifier", Type: TypeIPv4},
	55:  {Code: 55, Name: "parameter-request-list", Type: TypeBinary},
	57:  {Code: 57, Name: "max-dhcp-message-size", Type: TypeUint16},
	58:  {Code: 58, Name: "renewal-time-value", Type: TypeUint32},
	59:  {Code: 59, Name: "rebinding-time-value", Type: TypeUint32},
	60:  {Code: 60, Name: "vendor-class-identifier", Type: TypeString},
	61:  {Code: 61, Name: "client-identifier", Type: TypeBinary},
	66:  {Code: 66, Name: "tftp-server-name", Type: TypeString},
	67:  {Code: 67, Name: "bootfile-name", Type: TypeString},
	77:  {Code: 77, Name: "user-class", Type: TypeBinary},
	80:  {Code: 80, Name: "rapid-commit", Type: TypeBinary},
	82:  {Code: 82, Name: "relay-agent-information", Type: TypeBinary},
	93:  {Code: 93, Name: "client-system-architecture", Type: TypeUint16},
	121: {Code: 121, Name: "classless-static-route", Type: TypeRouteList},
	150: {Code: 150, Name: "tftp-server-address", Type: TypeIPv4List},
	176: {Code: 176, Name: "avaya-phones", Type: TypeString},
}

func OptionName(code byte) string {
	if spec, ok := OptionSpecs[code]; ok {
		return spec.Name
	}
	return "unknown"
}
