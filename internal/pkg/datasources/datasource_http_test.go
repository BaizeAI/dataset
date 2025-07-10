// nolint: dupl
package datasources

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHTTPLoader(t *testing.T) {
	httpLoader, err := NewHTTPLoader(map[string]string{
		"branch": "master",
	}, Options{
		Type: "",
		URI:  "https://test.com",
		Path: "",
		Mode: 0,
		UID:  0,
		GID:  0,
		Root: "",
	}, Secrets{
		Username: "test-username",
		Password: "test-password",
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
	gitDir, _ := os.MkdirTemp("", "httpLoader-*")
	defer func() {
		assert.NoError(t, os.RemoveAll(gitDir))
	}()
	assert.NoError(t, err)
	fakeHTTP.WithContext(func() {
		err = httpLoader.Sync("http://test.com", gitDir)
		assert.NoError(t, err)
	})
	bbs := fakeHTTP.GetAllInputs()
	assert.Equal(t, []byte("config touch\n"), bbs[0])
	assert.True(t, strings.HasPrefix(string(bbs[1]), "config create"))
	assert.True(t, strings.HasPrefix(string(bbs[2]), "sync"))
}
