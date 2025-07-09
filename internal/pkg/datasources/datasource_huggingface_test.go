// nolint: dupl
package datasources

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHuggingFaceLoader(t *testing.T) {
	loader, err := NewHuggingFaceLoader(map[string]string{
		"endpoint": "https://example-hf.com",
	}, Options{
		Type: "",
		URI:  "huggingface://ns/model",
		Path: "",
		Mode: 0,
		UID:  0,
		GID:  0,
		Root: "",
	}, Secrets{
		Token: "test-token",
	})
	assert.NoError(t, err)
	fakeHTTP := fakeCommand{
		t:   t,
		cmd: "huggingface-cli",
		outputs: []out{
			{
				stdout: "env",
				stderr: "",
				exit:   0,
			},
			{
				stdout: "login",
				stderr: "",
				exit:   0,
			},
			{
				stdout: "whoami",
				stderr: "",
				exit:   0,
			},
			{
				stdout: "download",
				stderr: "",
				exit:   0,
			},
		},
	}
	defer func() {
		assert.NoError(t, fakeHTTP.Clean())
	}()
	huggingFaceDir, _ := os.MkdirTemp("", "huggingFaceLoader-*")
	defer func() {
		assert.NoError(t, os.RemoveAll(huggingFaceDir))
	}()
	assert.NoError(t, err)
	fakeHTTP.WithContext(func() {
		err = loader.Sync("huggingface://ns/model", huggingFaceDir)
		assert.NoError(t, err)
	})
	bbs := fakeHTTP.GetAllInputs()
	require.Len(t, bbs, 4)
	assert.Equal(t, []byte("env\n"), bbs[0])
	assert.Equal(t, string(bbs[1]), "login --token test-token\n")
	assert.Equal(t, string(bbs[2]), "whoami\n")
	assert.Equal(t, string(bbs[3]), strings.Join([]string{"download", "ns/model", "--local-dir", huggingFaceDir, "--resume-download"}, " ")+"\n")
}

func TestHuggingFaceLoaderWithBandwidthLimit(t *testing.T) {
	loader, err := NewHuggingFaceLoader(map[string]string{
		"endpoint":       "https://example-hf.com",
		"bandwidthLimit": "5M",
	}, Options{
		Type: "",
		URI:  "huggingface://ns/model",
		Path: "",
		Mode: 0,
		UID:  0,
		GID:  0,
		Root: "",
	}, Secrets{
		Token: "test-token",
	})
	assert.NoError(t, err)
	
	// Verify that the bandwidth limit is stored correctly
	assert.Equal(t, "5M", loader.huggingFaceOptions.BandwidthLimit)
}
