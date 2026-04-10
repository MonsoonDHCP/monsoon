package dhcpv4

func HasRapidCommit(opts Options) bool {
	_, ok := opts[OptionRapidCommit]
	return ok
}

func EnableRapidCommit(opts Options) {
	opts[OptionRapidCommit] = []byte{}
}
