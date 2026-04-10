package dhcpv4

type MatchContext struct {
	MACPrefix   string
	VendorClass string
	UserClass   string
	CircuitID   string
}

type ClassRule struct {
	Name        string
	VendorClass string
	MACPrefix   string
	UserClass   string
	CircuitID   string
	Pool        string
}

type Classifier struct {
	rules []ClassRule
}

func NewClassifier(rules []ClassRule) *Classifier {
	return &Classifier{rules: append([]ClassRule(nil), rules...)}
}

func (c *Classifier) Match(ctx MatchContext) string {
	for _, r := range c.rules {
		if r.VendorClass != "" && !wildcardMatch(r.VendorClass, ctx.VendorClass) {
			continue
		}
		if r.MACPrefix != "" && !hasPrefixFold(ctx.MACPrefix, r.MACPrefix) {
			continue
		}
		if r.UserClass != "" && !wildcardMatch(r.UserClass, ctx.UserClass) {
			continue
		}
		if r.CircuitID != "" && !wildcardMatch(r.CircuitID, ctx.CircuitID) {
			continue
		}
		return r.Pool
	}
	return ""
}

func hasPrefixFold(s, prefix string) bool {
	if len(prefix) > len(s) {
		return false
	}
	return equalFold(s[:len(prefix)], prefix)
}

func wildcardMatch(pattern, value string) bool {
	if pattern == "" {
		return true
	}
	if pattern == "*" {
		return true
	}
	if len(pattern) > 0 && pattern[len(pattern)-1] == '*' {
		return hasPrefixFold(value, pattern[:len(pattern)-1])
	}
	return equalFold(pattern, value)
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
