package datasources

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitLoaderSyncFromUnbornRepository(t *testing.T) {
	remoteDir, branch := createBareGitRemote(t)

	for _, testCase := range []struct {
		name              string
		withStaleHEADLock bool
	}{
		{name: "pulls initial commit"},
		{name: "removes stale HEAD lock and pulls initial commit", withStaleHEADLock: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			rootDir := t.TempDir()
			t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(rootDir, "gitconfig"))
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

			repoDir := filepath.Join(rootDir, "repository")
			require.NoError(t, os.Mkdir(repoDir, 0755))
			runGit(t, repoDir, "init")

			headLockPath := filepath.Join(repoDir, ".git", "HEAD.lock")
			if testCase.withStaleHEADLock {
				require.NoError(t, os.WriteFile(headLockPath, nil, 0600))
			}

			loader, err := NewGitLoader(map[string]string{"branch": branch}, Options{Root: rootDir}, Secrets{})
			require.NoError(t, err)
			require.NoError(t, loader.Sync(remoteDir, "repository"))

			assert.NotEmpty(t, strings.TrimSpace(runGit(t, repoDir, "rev-parse", "--verify", "HEAD")))
			assert.Equal(t, "initial content\n", string(requireFileContents(t, filepath.Join(repoDir, "README.md"))))
			if testCase.withStaleHEADLock {
				assert.NoFileExists(t, headLockPath)
			}
		})
	}
}

func TestGitLoaderSyncResetsDivergedRepository(t *testing.T) {
	remoteDir, branch := createBareGitRemote(t)
	rootDir := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(rootDir, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	loader, err := NewGitLoader(map[string]string{"branch": branch}, Options{Root: rootDir}, Secrets{})
	require.NoError(t, err)
	require.NoError(t, loader.Sync(remoteDir, "repository"))

	repoDir := filepath.Join(rootDir, "repository")
	runGit(t, repoDir, "config", "user.name", "test user")
	runGit(t, repoDir, "config", "user.email", "test@example.com")
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("local change\n"), 0600))
	runGit(t, repoDir, "add", "README.md")
	runGit(t, repoDir, "commit", "-m", "local divergent commit")

	require.NoError(t, loader.Sync(remoteDir, "repository"))
	assert.Equal(t, "initial content\n", string(requireFileContents(t, filepath.Join(repoDir, "README.md"))))
}

func createBareGitRemote(t *testing.T) (string, string) {
	t.Helper()

	rootDir := t.TempDir()
	sourceDir := filepath.Join(rootDir, "source")
	remoteDir := filepath.Join(rootDir, "remote.git")
	require.NoError(t, os.Mkdir(sourceDir, 0755))
	runGit(t, sourceDir, "init")
	runGit(t, sourceDir, "config", "user.name", "test user")
	runGit(t, sourceDir, "config", "user.email", "test@example.com")
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "README.md"), []byte("initial content\n"), 0600))
	runGit(t, sourceDir, "add", "README.md")
	runGit(t, sourceDir, "commit", "-m", "initial commit")
	branch := strings.TrimSpace(runGit(t, sourceDir, "branch", "--show-current"))

	runGit(t, rootDir, "init", "--bare", remoteDir)
	runGit(t, sourceDir, "remote", "add", "origin", remoteDir)
	runGit(t, sourceDir, "push", "origin", branch)

	return remoteDir, branch
}

func runGit(t *testing.T, directory string, args ...string) string {
	t.Helper()

	command := exec.Command("git", args...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	require.NoErrorf(t, err, "git %s failed:\n%s", strings.Join(args, " "), output)

	return string(output)
}

func requireFileContents(t *testing.T, filePath string) []byte {
	t.Helper()

	contents, err := os.ReadFile(filePath)
	require.NoError(t, err)
	return contents
}
func TestGitLoader(t *testing.T) {
	t.Run("removes stale HEAD lock before pull", func(t *testing.T) {
		git, err := NewGitLoader(map[string]string{}, Options{}, Secrets{})
		require.NoError(t, err)

		gitDir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(gitDir, ".git"), 0755))
		headLockPath := filepath.Join(gitDir, ".git", "HEAD.lock")
		require.NoError(t, os.WriteFile(headLockPath, nil, 0600))

		removed, err := git.removeStaleHEADLock(gitDir)
		require.NoError(t, err)
		assert.True(t, removed)
		assert.NoFileExists(t, headLockPath)
	})

	t.Run("does not remove non-regular HEAD lock", func(t *testing.T) {
		git, err := NewGitLoader(map[string]string{}, Options{}, Secrets{})
		require.NoError(t, err)

		gitDir := t.TempDir()
		headLockPath := filepath.Join(gitDir, ".git", "HEAD.lock")
		require.NoError(t, os.MkdirAll(headLockPath, 0755))

		removed, err := git.removeStaleHEADLock(gitDir)
		assert.False(t, removed)
		require.ErrorContains(t, err, "refusing to remove non-regular Git HEAD lock")
		assert.DirExists(t, headLockPath)
	})
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
					stdout: "head",
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
		assert.Contains(t, string(bbs[6]), "fetch")
		assert.Equal(t, "reset --hard FETCH_HEAD\n", string(bbs[7]))
		bbs[5] = []byte{}
		bbs[6] = []byte{}
		bbs[7] = []byte{}
		assert.Equal(t, [][]byte{
			[]byte("config --global safe.directory *\n"),
			[]byte("rev-parse --verify HEAD\n"),
			[]byte("update-index --refresh\n"),
			[]byte("add -u\n"),
			[]byte("stash\n"),
			{},
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
					stdout: "head",
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
		assert.Contains(t, string(bbs[6]), "branch --show-current")
		assert.Contains(t, string(bbs[7]), "fetch")
		assert.Equal(t, "reset --hard FETCH_HEAD\n", string(bbs[8]))
		bbs[5] = []byte{}
		bbs[6] = []byte{}
		bbs[7] = []byte{}
		bbs[8] = []byte{}
		assert.Equal(t, [][]byte{
			[]byte("config --global safe.directory *\n"),
			[]byte("rev-parse --verify HEAD\n"),
			[]byte("update-index --refresh\n"),
			[]byte("add -u\n"),
			[]byte("stash\n"),
			{},
			{},
			{},
			{},
		}, bbs)
	})
}
