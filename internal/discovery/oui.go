package discovery

import "strings"

var ouiVendors = map[string]string{
	"000C29": "VMware, Inc.",
	"00163E": "Cisco Systems",
	"001A2B": "Ayecom Technology Co., Ltd.",
	"001B63": "Apple, Inc.",
	"00236C": "Juniper Networks",
	"002590": "Super Micro Computer, Inc.",
	"0026B9": "Dell Inc.",
	"005056": "VMware, Inc.",
	"0080C2": "IEEE Registration Authority",
	"0C4DE9": "Hewlett Packard Enterprise",
	"3C52A1": "Hewlett Packard",
	"40A8F0": "Ubiquiti Inc",
	"44D9E7": "Dell Inc.",
	"485D60": "AzureWave Technology Inc.",
	"525400": "QEMU",
	"58EF68": "TP-Link Corporation Limited",
	"5C514F": "Intel Corporate",
	"5CF370": "Samsung Electronics Co.,Ltd",
	"7071BC": "Apple, Inc.",
	"8C8590": "Hikvision Digital Technology Co.,Ltd.",
	"9CFC01": "Espressif Inc.",
	"A4B197": "Amazon Technologies Inc.",
	"B827EB": "Raspberry Pi Foundation",
	"DCA632": "Ruckus Wireless",
	"F4F5D8": "Google, Inc.",
}

func LookupVendor(mac string) string {
	prefix := macPrefix(mac)
	if prefix == "" {
		return ""
	}
	return ouiVendors[prefix]
}

func macPrefix(mac string) string {
	cleaned := strings.ToUpper(strings.TrimSpace(mac))
	cleaned = strings.NewReplacer(":", "", "-", "", ".", "").Replace(cleaned)
	if len(cleaned) < 6 {
		return ""
	}
	for _, ch := range cleaned[:6] {
		if (ch < '0' || ch > '9') && (ch < 'A' || ch > 'F') {
			return ""
		}
	}
	return cleaned[:6]
}
