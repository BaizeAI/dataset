package s3tls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
)

func TestValidateCA(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1)}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	valid := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	bad := "-----BEGIN CERTIFICATE-----\n@@\n-----END CERTIFICATE-----\n"
	cases := []struct {
		name, data string
		ok         bool
	}{
		{"one", valid, true}, {"duplicate", valid + valid, true},
		{"CRLF", strings.ReplaceAll(valid, "\n", "\r\n"), true},
		{"boundary", valid + strings.Repeat(" ", MaxCABytes-len(valid)), true},
		{"too large", valid + strings.Repeat(" ", MaxCABytes-len(valid)+1), false},
		{"empty", "", false}, {"whitespace", " \n\t", false},
		{"private key", valid + "-----BEGIN PRIVATE KEY-----\nAQ==\n-----END PRIVATE KEY-----", false},
		{"leading garbage", "garbage" + valid, false}, {"trailing garbage", valid + "garbage", false},
		{"bad first", bad + valid, false}, {"bad middle", valid + bad + valid, false}, {"bad last", valid + bad, false},
		{"nested begin", "-----BEGIN CERTIFICATE-----\n" + valid, false},
		{"invalid DER", "-----BEGIN CERTIFICATE-----\nAQ==\n-----END CERTIFICATE-----", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := ValidateCA([]byte(c.data)); (err == nil) != c.ok {
				t.Fatalf("valid=%v, error=%v", c.ok, err)
			}
		})
	}
}

func TestEndpointContract(t *testing.T) {
	for _, c := range []struct {
		mode, provider, endpoint string
		ok                       bool
	}{
		{"", "MINIO", "old-endpoint:9000", true}, {System, "MINIO", "http://host", true},
		{CustomCA, "AWS", "", true}, {InsecureSkipVerify, "AWS", "", true},
		{CustomCA, "MINIO", "https://host:9443", true}, {CustomCA, "MINIO", "https://[::1]:443", true},
		{CustomCA, "MINIO", "", false}, {CustomCA, "MINIO", "http://host", false},
		{CustomCA, "MINIO", "https://", false}, {CustomCA, "MINIO", "https://host:abc", false},
		{CustomCA, "MINIO", "https://host:65536", false}, {CustomCA, "MINIO", "https://host:", false},
		{"unknown", "AWS", "", false},
	} {
		if err := ValidateEndpoint(c.mode, c.provider, c.endpoint); (err == nil) != c.ok {
			t.Errorf("%+v: %v", c, err)
		}
	}
}
