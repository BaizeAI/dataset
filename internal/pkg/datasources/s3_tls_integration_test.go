package datasources

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/md5" // Test object checksum for the S3 protocol.
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Run with S3_TLS_INTEGRATION=1 and the image's rclone v1.70.1 on PATH.
// This exercises actual HTTPS and S3 transfers without external credentials.
func TestS3TLSIntegration(t *testing.T) {
	if os.Getenv("S3_TLS_INTEGRATION") != "1" {
		t.Skip("set S3_TLS_INTEGRATION=1 to run the pinned rclone HTTPS fixture")
	}
	version, err := exec.Command("rclone", "version").Output()
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(version), "rclone v1.70.1\n"), "use the data-loader's pinned rclone v1.70.1")
	t.Setenv("RCLONE_CONFIG", filepath.Join(t.TempDir(), "rclone.conf"))
	t.Setenv("RCLONE_RETRIES", "1")
	t.Setenv("RCLONE_LOW_LEVEL_RETRIES", "1")
	t.Setenv("RCLONE_RETRIES_SLEEP", "10ms")
	t.Setenv("RCLONE_CONNECT_TIMEOUT", "2s")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("RCLONE_NO_CHECK_CERTIFICATE", "true")
	t.Setenv("RCLONE_CA_CERT", "/invalid/environment/ca")
	for _, operation := range []string{"copy", "sync"} {
		for _, scenario := range []struct {
			name, mode                                string
			wrongCA, badSAN, expired, systemTrust, ok bool
		}{
			{name: "system trusted", mode: "system", systemTrust: true, ok: true},
			{name: "system untrusted", mode: "system"},
			{name: "custom trusted", mode: "customCA", ok: true},
			{name: "custom wrong CA", mode: "customCA", wrongCA: true},
			{name: "custom replaces system pool", mode: "customCA", wrongCA: true, systemTrust: true},
			{name: "custom SAN mismatch", mode: "customCA", badSAN: true},
			{name: "custom expired", mode: "customCA", expired: true},
			{name: "insecure explicit", mode: "insecureSkipVerify", badSAN: true, expired: true, ok: true},
		} {
			t.Run(operation+"/"+scenario.name, func(t *testing.T) {
				cert, bundle := s3IntegrationCertificate(t, scenario.badSAN, scenario.expired)
				payload := "dataset HTTPS TLS fixture\n"
				checksum := fmt.Sprintf("%x", md5.Sum([]byte(payload)))
				server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/bucket":
						w.Header().Set("Content-Type", "application/xml")
						if _, err := fmt.Fprintf(w, `<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>bucket</Name><Prefix></Prefix><KeyCount>1</KeyCount><MaxKeys>1000</MaxKeys><IsTruncated>false</IsTruncated><Contents><Key>object.txt</Key><LastModified>2026-01-01T00:00:00.000Z</LastModified><ETag>"%s"</ETag><Size>%d</Size><StorageClass>STANDARD</StorageClass></Contents></ListBucketResult>`, checksum, len(payload)); err != nil {
							t.Errorf("write S3 bucket listing: %v", err)
						}
					case "/bucket/object.txt":
						w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
						w.Header().Set("ETag", `"`+checksum+`"`)
						w.Header().Set("Last-Modified", "Thu, 01 Jan 2026 00:00:00 GMT")
						if r.Method != "HEAD" {
							if _, err := io.WriteString(w, payload); err != nil {
								t.Errorf("write S3 object: %v", err)
							}
						}
					default:
						http.NotFound(w, r)
					}
				}))
				server.Config.ErrorLog = log.New(io.Discard, "", 0)
				server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
				server.StartTLS()
				defer server.Close()
				root := t.TempDir()
				ca := filepath.Join(root, "ca.crt")
				trusted := filepath.Join(root, "system.crt")
				require.NoError(t, os.WriteFile(trusted, bundle, 0600))
				if scenario.wrongCA {
					_, bundle = s3IntegrationCertificate(t, false, false)
				}
				require.NoError(t, os.WriteFile(ca, bundle, 0600))
				if scenario.systemTrust {
					t.Setenv("SSL_CERT_FILE", trusted)
				} else {
					t.Setenv("SSL_CERT_FILE", filepath.Join(root, "missing"))
				}
				t.Setenv("SSL_CERT_DIR", root)
				// SSL_CERT_DIR must not contain the uploaded CA: that would trust it in SYSTEM.
				emptySystem := filepath.Join(root, "system-dir")
				require.NoError(t, os.Mkdir(emptySystem, 0700))
				t.Setenv("SSL_CERT_DIR", emptySystem)
				options := map[string]string{"provider": "MINIO", "endpoint": server.URL, "syncMode": operation, "tlsMode": scenario.mode}
				if scenario.mode == "customCA" {
					options["tlsCAFile"] = ca
				}
				destination := filepath.Join(root, "data")
				require.NoError(t, os.Mkdir(destination, 0700))
				loader, err := NewS3Loader(options, Options{URI: "s3://bucket", Root: root}, Secrets{AKSKAccessKeyID: "fixture-access", AKSKSecretAccessKey: "fixture-secret"})
				require.NoError(t, err)
				err = loader.Sync("s3://bucket", destination)
				if scenario.ok {
					require.NoError(t, err)
					data, err := os.ReadFile(filepath.Join(destination, "object.txt"))
					require.NoError(t, err)
					require.Equal(t, payload, string(data))
				} else {
					require.Error(t, err)
					_, err = os.Stat(filepath.Join(destination, "object.txt"))
					require.True(t, os.IsNotExist(err))
				}
			})
		}
	}
}

func s3IntegrationCertificate(t *testing.T, badSAN, expired bool) (tls.Certificate, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	cert := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	if badSAN {
		cert.DNSNames = []string{"wrong.example"}
	} else {
		cert.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	}
	if expired {
		cert.NotBefore = time.Now().Add(-2 * time.Hour)
		cert.NotAfter = time.Now().Add(-time.Hour)
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	require.NoError(t, err)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
