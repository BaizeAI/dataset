// nolint: dupl
package datasources

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelScopeLoader(t *testing.T) {
	loader, err := NewModelScopeLoader(map[string]string{}, Options{
		Type: "",
		URI:  "modelscope://ns/model",
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
		cmd: "modelscope",
		outputs: []out{
			{
				stdout: "login",
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
	defer fakeHTTP.Clean()
	modelScopeDir, _ := os.MkdirTemp("", "modelScopeLoader-*")
	defer os.RemoveAll(modelScopeDir)
	assert.NoError(t, err)
	fakeHTTP.WithContext(func() {
		err = loader.Sync("modelscope://ns/model", modelScopeDir)
		assert.NoError(t, err)
	})
	bbs := fakeHTTP.GetAllInputs()
	require.Len(t, bbs, 2)
	assert.Equal(t, string(bbs[0]), "login --token test-token\n")
	assert.Equal(t, string(bbs[1]), strings.Join([]string{"download", "ns/model", "--local_dir", modelScopeDir}, " ")+"\n")
}
