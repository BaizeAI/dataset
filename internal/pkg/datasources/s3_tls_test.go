package datasources

import (
	"encoding/pem"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func tlsTestCA(t *testing.T) string {
	t.Helper()
	server := httptest.NewTLSServer(nil)
	defer server.Close()
	path := filepath.Join(t.TempDir(), "ca.crt")
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600))
	return path
}

func TestS3TLSValidation(t *testing.T) {
	ca := tlsTestCA(t)
	for _, c := range []struct {
		mode, file, endpoint, provider string
		ok                             bool
	}{
		{"", "", "legacy-host", "MINIO", true}, {"system", "", "http://host", "MINIO", true},
		{"customCA", ca, "https://host", "MINIO", true}, {"customCA", ca, "", "AWS", true},
		{"insecureSkipVerify", "", "", "AWS", true}, {"insecureSkipVerify", "", "https://host", "MINIO", true},
		{"customCA", "", "https://host", "MINIO", false}, {"customCA", ca + ".missing", "https://host", "MINIO", false},
		{"customCA", ca, "http://host", "MINIO", false}, {"system", ca, "https://host", "MINIO", false},
		{"insecureSkipVerify", ca, "https://host", "MINIO", false}, {"unknown", "", "https://host", "MINIO", false},
	} {
		_, err := NewS3Loader(map[string]string{"provider": c.provider, "region": "us-east-1", "endpoint": c.endpoint, "tlsMode": c.mode, "tlsCAFile": c.file}, Options{}, Secrets{})
		require.Equal(t, c.ok, err == nil, "%+v: %v", c, err)
	}
	require.NoError(t, os.WriteFile(ca, []byte("invalid"), 0600))
	_, err := NewS3Loader(map[string]string{"provider": "AWS", "region": "us-east-1", "tlsMode": "customCA", "tlsCAFile": ca}, Options{}, Secrets{})
	require.Error(t, err)
}

func TestS3TLSTransferCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable shell fixture requires Linux")
	}
	ca := tlsTestCA(t)
	bin := t.TempDir()
	capture := filepath.Join(bin, "capture")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$CAPTURE\"\nenv >> \"$CAPTURE\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(bin, "rclone"), []byte(script), 0700))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CAPTURE", capture)
	t.Setenv("RCLONE_NO_CHECK_CERTIFICATE", "true")
	t.Setenv("RCLONE_CA_CERT", "/bad/ca")
	t.Setenv("HTTPS_PROXY", "http://proxy")
	t.Setenv("SSL_CERT_FILE", "/system/ca")
	for _, mode := range []string{"system", "customCA", "insecureSkipVerify"} {
		for _, operation := range []string{"copy", "sync"} {
			require.NoError(t, os.WriteFile(capture, nil, 0600))
			options := map[string]string{"provider": "AWS", "region": "us-east-1", "syncMode": operation, "tlsMode": mode}
			if mode == "customCA" {
				options["tlsCAFile"] = ca
			}
			loader, err := NewS3Loader(options, Options{URI: "s3://bucket/path", Root: bin}, Secrets{AKSKAccessKeyID: "access", AKSKSecretAccessKey: "secret"})
			require.NoError(t, err)
			require.NoError(t, loader.Sync("s3://bucket/path", bin))
			bytes, err := os.ReadFile(capture)
			require.NoError(t, err)
			output := string(bytes)
			require.Contains(t, output, operation+" baize-data-loader-copy-config-")
			require.NotContains(t, output, "RCLONE_NO_CHECK_CERTIFICATE=")
			require.NotContains(t, output, "RCLONE_CA_CERT=")
			require.Contains(t, output, "HTTPS_PROXY=http://proxy")
			require.Contains(t, output, "SSL_CERT_FILE=/system/ca")
			require.Contains(t, output, "RCLONE_S3_ACCESS_KEY_ID=access")
			if mode == "customCA" {
				require.Contains(t, output, "--ca-cert "+ca)
			} else {
				require.NotContains(t, output, "--ca-cert ")
			}
			require.Contains(t, output, "--no-check-certificate="+map[bool]string{true: "true", false: "false"}[mode == "insecureSkipVerify"])
		}
	}
}

func TestS3TLSEnvironmentPreservesOtherSettings(t *testing.T) {
	result := s3TLSEnvironment([]string{"RCLONE_NO_CHECK_CERTIFICATE=true", "RCLONE_CA_CERT=x", "HTTPS_PROXY=proxy", "AWS_ACCESS_KEY_ID=key", "SSL_CERT_FILE=system"})
	require.Equal(t, "HTTPS_PROXY=proxy,AWS_ACCESS_KEY_ID=key,SSL_CERT_FILE=system", strings.Join(result, ","))
}
