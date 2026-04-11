package main

import (
	"crypto/tls"
	"strings"
)

func tlsFilesConfigured(certFile, keyFile string) bool {
	return strings.TrimSpace(certFile) != "" && strings.TrimSpace(keyFile) != ""
}

func loadServerTLSConfig(certFile, keyFile string) (*tls.Config, error) {
	if !tlsFilesConfigured(certFile, keyFile) {
		return nil, nil
	}

	cert, err := tls.LoadX509KeyPair(strings.TrimSpace(certFile), strings.TrimSpace(keyFile))
	if err != nil {
		return nil, err
	}

	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h2", "http/1.1"},
	}, nil
}
