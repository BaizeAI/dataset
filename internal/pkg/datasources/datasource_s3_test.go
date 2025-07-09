// nolint: dupl
package datasources

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestS3Loader(t *testing.T) {
	loader, err := NewS3Loader(map[string]string{
		"region": "us-east-1",
	}, Options{
		Type: "",
		URI:  "s3://test-bucket",
		Path: "",
		Mode: 0,
		UID:  0,
		GID:  0,
		Root: "",
	}, Secrets{
		AKSKAccessKeyID:     "accid",
		AKSKSecretAccessKey: "acckey",
	})
	assert.NoError(t, err)
	fakeHTTP := fakeCommand{
		t:   t,
		cmd: "rclone",
		outputs: []out{
			{
				stdout: "clone",
				stderr: "",
				exit:   0,
			},
			{
				stdout: "config",
				stderr: "",
				exit:   0,
			},
			{
				stdout: "config",
				stderr: "",
				exit:   0,
			},
		},
	}
	defer func() {
		assert.NoError(t, fakeHTTP.Clean())
	}()
	s3Dir, _ := os.MkdirTemp("", "s3Loader-*")
	defer func() {
		assert.NoError(t, os.RemoveAll(s3Dir))
	}()
	assert.NoError(t, err)
	fakeHTTP.WithContext(func() {
		err = loader.Sync("s3://test-bucket", s3Dir)
		assert.NoError(t, err)
	})
	bbs := fakeHTTP.GetAllInputs()
	assert.Equal(t, []byte("config touch\n"), bbs[0])
	assert.True(t, strings.HasPrefix(string(bbs[1]), "config create"))
	assert.True(t, strings.HasPrefix(string(bbs[2]), "sync"))
}

func TestS3LoaderWithBandwidthLimit(t *testing.T) {
	loader, err := NewS3Loader(map[string]string{
		"region":         "us-east-1",
		"bandwidthLimit": "5M",
	}, Options{
		Type: "",
		URI:  "s3://test-bucket",
		Path: "",
		Mode: 0,
		UID:  0,
		GID:  0,
		Root: "",
	}, Secrets{
		AKSKAccessKeyID:     "accid",
		AKSKSecretAccessKey: "acckey",
	})
	assert.NoError(t, err)
	fakeHTTP := fakeCommand{
		t:   t,
		cmd: "rclone",
		outputs: []out{
			{
				stdout: "clone",
				stderr: "",
				exit:   0,
			},
			{
				stdout: "config",
				stderr: "",
				exit:   0,
			},
			{
				stdout: "config",
				stderr: "",
				exit:   0,
			},
		},
	}
	defer func() {
		assert.NoError(t, fakeHTTP.Clean())
	}()
	s3Dir, _ := os.MkdirTemp("", "s3Loader-*")
	defer func() {
		assert.NoError(t, os.RemoveAll(s3Dir))
	}()
	assert.NoError(t, err)
	fakeHTTP.WithContext(func() {
		err = loader.Sync("s3://test-bucket", s3Dir)
		assert.NoError(t, err)
	})
	bbs := fakeHTTP.GetAllInputs()
	assert.Equal(t, []byte("config touch\n"), bbs[0])
	assert.True(t, strings.HasPrefix(string(bbs[1]), "config create"))
	syncCommand := string(bbs[2])
	assert.True(t, strings.HasPrefix(syncCommand, "sync"))
	assert.Contains(t, syncCommand, "--bwlimit")
	assert.Contains(t, syncCommand, "5M")
}
