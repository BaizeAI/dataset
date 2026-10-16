// Package s3tls validates the S3 TLS contract without contacting object storage.
package s3tls

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	System             = "system"
	CustomCA           = "customCA"
	InsecureSkipVerify = "insecureSkipVerify"
	MaxCABytes         = 64 * 1024
)

func Mode(mode string) (string, error) {
	switch mode {
	case "", System:
		return System, nil
	case CustomCA, InsecureSkipVerify:
		return mode, nil
	default:
		return "", fmt.Errorf("tls.mode: unknown TLS mode")
	}
}

func ValidateEndpoint(mode, provider, endpoint string) error {
	mode, err := Mode(mode)
	if err != nil {
		return err
	}
	if mode == System || (provider == "AWS" && endpoint == "") {
		return nil
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return fmt.Errorf("tls.mode: non-default TLS requires a valid HTTPS endpoint (or an empty AWS endpoint)")
	}
	if strings.HasSuffix(u.Host, ":") {
		return fmt.Errorf("endpoint: invalid port")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("endpoint: invalid port")
		}
	}
	return nil
}

// ValidateCA consumes every block. pem.Decode alone would skip malformed blocks
// and leading garbage, potentially accepting only part of an uploaded bundle.
func ValidateCA(data []byte) error {
	if len(data) == 0 || len(data) > MaxCABytes || !utf8.Valid(data) {
		return fmt.Errorf("tls.caCert: expected a UTF-8 PEM bundle of 1 to 65536 bytes")
	}
	rest := bytes.TrimSpace(data)
	count := 0
	for len(rest) > 0 {
		const begin = "-----BEGIN CERTIFICATE-----"
		const end = "-----END CERTIFICATE-----"
		if !bytes.HasPrefix(rest, []byte(begin)) {
			return fmt.Errorf("tls.caCert: only CERTIFICATE PEM blocks and whitespace are allowed")
		}
		i := bytes.Index(rest, []byte(end))
		if i < 0 {
			return fmt.Errorf("tls.caCert: malformed PEM block")
		}
		segment := rest[:i+len(end)]
		if bytes.Contains(segment[len(begin):], []byte("-----BEGIN")) {
			return fmt.Errorf("tls.caCert: malformed PEM block")
		}
		block, tail := pem.Decode(segment)
		if block == nil || len(tail) != 0 || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return fmt.Errorf("tls.caCert: invalid certificate PEM block")
		}
		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			return fmt.Errorf("tls.caCert: invalid X.509 certificate")
		}
		count++
		rest = bytes.TrimSpace(rest[i+len(end):])
	}
	if count == 0 {
		return fmt.Errorf("tls.caCert: at least one certificate is required")
	}
	return nil
}
