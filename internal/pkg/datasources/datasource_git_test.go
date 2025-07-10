package datasources

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitLoader(t *testing.T) {
	t.Run("clone", func(t *testing.T) {
		git, err := NewGitLoader(map[string]string{
			"branch": "master",
		}, Options{}, Secrets{
			Username: "test",
			Password: "password",
		})
		assert.NoError(t, err)
		fakeGit := fakeCommand{
			t:   t,
			cmd: "git",
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
				{
					stdout: "config",
					stderr: "",
					exit:   0,
				},
			},
		}
		defer func() {
			assert.NoError(t, fakeGit.Clean())
		}()
		gitDir, _ := os.MkdirTemp("", "git-*")
		defer func() {
			assert.NoError(t, os.RemoveAll(gitDir))
		}()
		assert.NoError(t, err)
		fakeGit.WithContext(func() {
			err = git.Sync("git://github.com/ndx-baize/baize.git", gitDir)
			assert.NoError(t, err)
		})
		bbs := fakeGit.GetAllInputs()
		assert.Equal(t, [][]byte{
			[]byte(fmt.Sprintf("clone git://github.com/ndx-baize/baize.git %s --branch master -v\n", gitDir)),
			[]byte("config --global safe.directory *\n"),
			[]byte("config --local core.fileMode false\n"),
			[]byte("remote set-url origin git://github.com/ndx-baize/baize.git\n"),
		}, bbs)
	})
	t.Run("checkout commit", func(t *testing.T) {
		git, err := NewGitLoader(map[string]string{
			"branch": "master",
			"commit": "12345",
		}, Options{}, Secrets{})
		assert.NoError(t, err)
		fakeGit := fakeCommand{
			t:   t,
			cmd: "git",
			outputs: []out{
				{
					stdout: "ok",
					stderr: "",
					exit:   0,
				},
				{
					stdout: "ok",
					stderr: "",
					exit:   0,
				},
				{
					stdout: "ok",
					stderr: "",
					exit:   0,
				},
				{
					stdout: "ok",
					stderr: "",
					exit:   0,
				},
			},
		}
		defer func() {
			assert.NoError(t, fakeGit.Clean())
		}()
		gitDir, _ := os.MkdirTemp("", "git-*")
		defer func() {
			assert.NoError(t, os.RemoveAll(gitDir))
		}()
		assert.NoError(t, err)
		fakeGit.WithContext(func() {
			err = git.Sync("git://github.com/ndx-baize/baize.git", gitDir)
			assert.NoError(t, err)
		})
		bbs := fakeGit.GetAllInputs()
		assert.Equal(t, [][]byte{
			[]byte(fmt.Sprintf("clone git://github.com/ndx-baize/baize.git %s --branch master -v\n", gitDir)),
			[]byte("config --global safe.directory *\n"),
			[]byte("config --local core.fileMode false\n"),
			[]byte("checkout 12345\n"),
		}, bbs)
	})
	t.Run("pull w/ branch", func(t *testing.T) {
		git, err := NewGitLoader(map[string]string{
			"branch": "master",
		}, Options{}, Secrets{})
		assert.NoError(t, err)
		fakeGit := fakeCommand{
			t:   t,
			cmd: "git",
			outputs: []out{
				{
					stdout: "config",
					stderr: "",
					exit:   0,
				},
				{
					stdout: "update",
					stderr: "",
					exit:   0,
				},
				{
					stdout: "add",
					stderr: "",
					exit:   0,
				},
				{
					stdout: "stash",
					stderr: "",
					exit:   0,
				},
				{
					stdout: "reset",
					stderr: "",
					exit:   0,
				},
				{
					stdout: "ok",
					stderr: "",
					exit:   0,
				},
				{
					stdout: "ok",
					stderr: "",
					exit:   0,
				},
			},
		}
		defer func() {
			assert.NoError(t, fakeGit.Clean())
		}()
		gitDir, _ := os.MkdirTemp("", "git-*")
		defer func() {
			assert.NoError(t, os.RemoveAll(gitDir))
		}()
		require.NoError(t, os.Mkdir(gitDir+"/.git", 0755))
		assert.NoError(t, err)
		fakeGit.WithContext(func() {
			err = git.Sync("git://github.com/ndx-baize/baize.git", gitDir)
			assert.NoError(t, err)
		})
		bbs := fakeGit.GetAllInputs()
		assert.Contains(t, string(bbs[5]), "remote add")
		assert.Contains(t, string(bbs[6]), "pull")
		bbs[5] = []byte{}
		bbs[6] = []byte{}
		assert.Equal(t, [][]byte{
			[]byte("config --global safe.directory *\n"),
			[]byte("update-index --refresh\n"),
			[]byte("add -u\n"),
			[]byte("stash\n"),
			[]byte("reset --hard master\n"),
			{},
			{},
		}, bbs)
	})
	t.Run("pull w/o branch", func(t *testing.T) {
		git, err := NewGitLoader(map[string]string{}, Options{}, Secrets{})
		assert.NoError(t, err)
		fakeGit := fakeCommand{
			t:   t,
			cmd: "git",
			outputs: []out{
				{
					stdout: "config",
					stderr: "",
					exit:   0,
				},
				{
					stdout: "update",
					stderr: "",
					exit:   0,
				},
				{
					stdout: "add",
					stderr: "",
					exit:   0,
				},
				{
					stdout: "stash",
					stderr: "",
					exit:   0,
				},
				{
					stdout: "reset",
					stderr: "",
					exit:   0,
				},
				{
					stdout: "ok",
					stderr: "",
					exit:   0,
				},
				{
					stdout: "branch1",
					stderr: "",
					exit:   0,
				},
				{
					stdout: "ok",
					stderr: "",
					exit:   0,
				},
			},
		}
		defer func() {
			assert.NoError(t, fakeGit.Clean())
		}()
		gitDir, _ := os.MkdirTemp("", "git-*")
		defer func() {
			assert.NoError(t, os.RemoveAll(gitDir))
		}()
		require.NoError(t, os.Mkdir(gitDir+"/.git", 0755))
		assert.NoError(t, err)
		fakeGit.WithContext(func() {
			err = git.Sync("git://github.com/ndx-baize/baize.git", gitDir)
			assert.NoError(t, err)
		})
		bbs := fakeGit.GetAllInputs()
		assert.Contains(t, string(bbs[5]), "remote add")
		bbs[5] = []byte{}
		assert.Contains(t, string(bbs[7]), "branch1")
		bbs[7] = []byte{}
		assert.Equal(t, [][]byte{
			[]byte("config --global safe.directory *\n"),
			[]byte("update-index --refresh\n"),
			[]byte("add -u\n"),
			[]byte("stash\n"),
			[]byte("reset --hard origin/HEAD\n"),
			{},
			[]byte("branch --show-current\n"),
			{},
		}, bbs)
	})
}
