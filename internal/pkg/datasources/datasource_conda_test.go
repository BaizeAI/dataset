package datasources

import (
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/BaizeAI/dataset/internal/pkg/constants"
	"github.com/BaizeAI/dataset/pkg/log"
)

func TestCondaSync(t *testing.T) {
	t.Run("sync full", func(t *testing.T) {
		temDir, err := os.MkdirTemp("", "test-data-*")
		require.NoError(t, err)
		defer func() {
			assert.NoError(t, os.RemoveAll(temDir))
		}()
		require.NoError(t, os.MkdirAll(temDir+"/test-env/conda/pkgs", 0700))
		require.NoError(t, os.MkdirAll(temDir+"/root", 0700))
		require.NoError(t, os.WriteFile(temDir+"/environment.yml", []byte("name: test-env\n"), 0600))
		require.NoError(t, os.WriteFile(temDir+"/requirements.txt", []byte("foo\nbar\nbaz\n"), 0600))
		condaLoader, err := NewCondaLoader(map[string]string{
			"name":                    "test-env",
			"pythonVersion":           "999.999.999",
			"pipIndexUrl":             "https://example.com/index-url",
			"pipExtraIndexUrl":        "https://example.com/index-url",
			"condaEnvironmentYmlPath": temDir + "/environment.yml",
			"pipRequirementsTxtPath":  temDir + "/requirements.txt",
			"condaPrefixDir":          temDir,
		}, Options{
			Root: temDir + "/root",
		}, Secrets{})
		assert.NoError(t, err)
		fakeConda := fakeCommand{
			t:   t,
			cmd: "mamba",
			outputs: []out{
				{
					stdout: "conda-v1",
					stderr: "",
					exit:   0,
				},
				{
					// conda info --json
					stdout: `{
  "GID": 0,
  "UID": 1000,
  "active_prefix": "/opt/conda"
}
`,
					stderr: "",
					exit:   0,
				},
				{
					// conda env list --json
					stdout: `
{
  "envs": [
    "/opt/conda",
    "/opt/conda/envs/baize-base"
  ]
}
`,
					stderr: "",
					exit:   0,
				},
				{
					// conda env create --file /path/to/environment.yml
					stdout: "env create out",
					stderr: "",
					exit:   0,
				},
				{
					// conda clean --all -y
					stdout: "clean",
					stderr: "",
					exit:   0,
				},
			},
		}
		defer func() {
			assert.NoError(t, fakeConda.Clean())
		}()
		fakePip := fakeCommand{
			t:    t,
			cmd:  "pip",
			path: path.Join(condaLoader.loaderOptions.envPrefix(), "bin"),
			outputs: []out{
				{
					// pip install -r /path/to/requirements.txt
					stdout: "pip out",
					stderr: "",
					exit:   0,
				},
			},
		}
		assert.NoError(t, err)
		defer func() {
			assert.NoError(t, fakePip.Clean())
		}()
		fakeRclone := fakeCommand{
			t:   t,
			cmd: "rclone",
			outputs: []out{
				{
					// rclone copyto
					stdout: "",
					stderr: "",
					exit:   0,
				},
				{
					// rclone copyto
					stdout: "",
					stderr: "",
					exit:   0,
				},
			},
		}
		defer func() {
			assert.NoError(t, fakeRclone.Clean())
		}()
		fakeConda.WithContext(func() {
			fakePip.WithContext(func() {
				fakeRclone.WithContext(func() {
					err = condaLoader.Sync("", "")
					assert.NoError(t, err)
				})
			})
		})
		pipInputs := fakePip.GetAllInputs()
		require.Len(t, pipInputs, 1)
		assert.Contains(t, string(pipInputs[0]), temDir+"/requirements.txt")

		rcloneInputs := fakeRclone.GetAllInputs()
		require.Len(t, rcloneInputs, 2)
		assert.Contains(t, string(rcloneInputs[0]), condaLoader.loaderOptions.prefixingPkgsDir)
		assert.Contains(t, string(rcloneInputs[0]), condaLoader.loaderOptions.finalPkgsDir)
		assert.Contains(t, string(rcloneInputs[1]), condaLoader.loaderOptions.prefixingEnvsDir)
		assert.Contains(t, string(rcloneInputs[1]), condaLoader.loaderOptions.finalEnvsDir)
		bbs := fakeConda.GetAllInputs()
		assert.Contains(t, string(bbs[3]), "env create --file")
		bbs[3] = nil
		assert.Equal(t, [][]byte{
			[]byte("--version\n"),
			[]byte("info --json\n"),
			[]byte("env list --json\n"),
			nil,
			[]byte("clean --all -y\n"),
		}, bbs)
	})
}

func TestParseOptionsFromOptions(t *testing.T) {
	l := new(CondaLoader)
	options, err := l.loaderOptions.parseOptionsFromOptions(map[string]string{}, Options{})
	require.Error(t, err)
	require.Empty(t, options)
	assert.EqualError(t, err, "missing required options --options name=<env-name>")

	options, err = l.loaderOptions.parseOptionsFromOptions(map[string]string{
		"name": "test-env",
	}, Options{})
	require.NoError(t, err)
	assert.Equal(t, CondaLoaderOptions{
		Name:                    "test-env",
		CondaEnvironmentYmlPath: constants.DatasetJobCondaCondaEnvironmentYAMLPath,
		PipRequirementsTxtPath:  constants.DatasetJobCondaPipRequirementsTxtPath,
		CondaPrefixDir:          constants.DatasetJobCondaMountDir,
		prefixingPkgsDir:        filepath.Join(constants.DatasetJobCondaMountDir, "test-env", "conda", "pkgs"),
		prefixingEnvsDir:        filepath.Join(constants.DatasetJobCondaMountDir, "test-env", "conda", "envs"),
		finalPkgsDir:            "conda/pkgs",
		finalEnvsDir:            "conda/envs",
	}, options)

	options, err = l.loaderOptions.parseOptionsFromOptions(map[string]string{
		"name":                    "test-env",
		"pythonVersion":           "999.999.999",
		"pipIndexUrl":             "https://example.com/index-url",
		"pipExtraIndexUrl":        "https://example.com/index-url",
		"condaEnvironmentYmlPath": "/path/to/environment.yml",
		"pipRequirementsTxtPath":  "/path/to/requirements.txt",
		"condaPrefixDir":          "/path/to/prefix",
	}, Options{})
	require.NoError(t, err)
	assert.Equal(t, CondaLoaderOptions{
		Name:                    "test-env",
		PythonVersion:           "999.999.999",
		PipIndexURL:             "https://example.com/index-url",
		PipExtraIndexURL:        "https://example.com/index-url",
		CondaEnvironmentYmlPath: "/path/to/environment.yml",
		PipRequirementsTxtPath:  "/path/to/requirements.txt",
		CondaPrefixDir:          "/path/to/prefix",
		prefixingPkgsDir:        "/path/to/prefix/test-env/conda/pkgs",
		prefixingEnvsDir:        "/path/to/prefix/test-env/conda/envs",
		finalPkgsDir:            "conda/pkgs",
		finalEnvsDir:            "conda/envs",
	}, options)
}

func TestNewCondaLoader(t *testing.T) {
	temDir, err := os.MkdirTemp("", "test-data-*")
	require.NoError(t, err)
	require.NotEmpty(t, temDir)
	defer func() {
		assert.NoError(t, os.RemoveAll(temDir))
	}()

	err = os.WriteFile(temDir+"/environment.yml", []byte("name: test-env\n"), 0600)
	require.NoError(t, err)
	err = os.WriteFile(temDir+"/requirements.txt", []byte("foo\nbar\nbaz\n"), 0600)
	require.NoError(t, err)

	l, err := NewCondaLoader(map[string]string{
		"name":                    "test-env",
		"pythonVersion":           "999.999.999",
		"pipIndexUrl":             "https://example.com/index-url",
		"pipExtraIndexUrl":        "https://example.com/index-url",
		"condaEnvironmentYmlPath": temDir + "/environment.yml",
		"pipRequirementsTxtPath":  temDir + "/requirements.txt",
		"condaPrefixDir":          "/path/to/prefix",
	}, Options{}, Secrets{})
	require.NoError(t, err)
	assert.Equal(t, CondaLoaderOptions{
		Name:                    "test-env",
		PythonVersion:           "999.999.999",
		PipIndexURL:             "https://example.com/index-url",
		PipExtraIndexURL:        "https://example.com/index-url",
		CondaEnvironmentYmlPath: temDir + "/environment.yml",
		PipRequirementsTxtPath:  temDir + "/requirements.txt",
		CondaPrefixDir:          "/path/to/prefix",
		prefixingPkgsDir:        "/path/to/prefix/test-env/conda/pkgs",
		prefixingEnvsDir:        "/path/to/prefix/test-env/conda/envs",
		finalPkgsDir:            "conda/pkgs",
		finalEnvsDir:            "conda/envs",
	}, l.loaderOptions)
}

func TestWriteTemp(t *testing.T) {
	l, err := NewCondaLoader(map[string]string{
		"name": "test-env",
	}, Options{}, Secrets{})
	require.NoError(t, err)

	path, cleanup, err := l.writeTemp(log.WithField("test", "test"), "environment.yml", []byte("name: test-env\n"))
	require.NoError(t, err)
	require.NotEmpty(t, path)
	defer cleanup()

	require.FileExists(t, path)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "name: test-env\n", string(content))
}

func TestNormalizeEnvironmentYaml(t *testing.T) {
	t.Run("EmptyEnvironment", func(t *testing.T) {
		assignedEnvironment, err := normalizeEnvironmentYaml(
			make(map[string]any),
			"test-env-name",
			"999.999.999",
			"https://example.com/index-url",
			[]string{"https://sub.example.com/extra-index-url"},
			"/path/to/envs/test-env-name",
		)
		require.NoError(t, err)
		require.NotEmpty(t, assignedEnvironment)
		assert.Equal(t, map[string]any{
			"name": "test-env-name",
			"channels": []any{
				"defaults",
				"conda-forge",
			},
			"dependencies": []any{
				"python=999.999.999",
				"pip",
				"ipykernel",
				"nb_conda_kernels",
				"notebook",
				map[string][]any{
					"pip": {
						"--index-url https://example.com/index-url",
						"--extra-index-url https://sub.example.com/extra-index-url",
						"--trusted-host example.com",
						"--trusted-host sub.example.com",
					},
				},
			},
			"default_threads": 4,
			"prefix":          "/path/to/envs/test-env-name",
		}, assignedEnvironment)

		_, err = yaml.Marshal(assignedEnvironment)
		require.NoError(t, err)
	})

	t.Run("Environment", func(t *testing.T) {
		assignedEnvironment, err := normalizeEnvironmentYaml(map[string]any{
			"name": "test-env-name",
			"channels": []any{
				"foo",
				"bar",
				"baz",
			},
			"dependencies": []any{
				"foo",
				"bar",
				"baz",
			},
		},
			"test-env-name",
			"999.999.999",
			"https://example.com/index-url",
			[]string{"https://sub.example.com/extra-index-url"},
			"/path/to/envs/test-env-name",
		)
		require.NoError(t, err)
		require.NotEmpty(t, assignedEnvironment)

		assert.Equal(t, map[string]any{
			"name": "test-env-name",
			"channels": []any{
				"foo",
				"bar",
				"baz",
			},
			"dependencies": []any{
				"foo",
				"bar",
				"baz",
				"python=999.999.999",
				"pip",
				"ipykernel",
				"nb_conda_kernels",
				"notebook",
				map[string][]any{
					"pip": {
						"--index-url https://example.com/index-url",
						"--extra-index-url https://sub.example.com/extra-index-url",
						"--trusted-host example.com",
						"--trusted-host sub.example.com",
					},
				},
			},
			"default_threads": 4,
			"prefix":          "/path/to/envs/test-env-name",
		}, assignedEnvironment)
	})
}

func TestAssignEssentialDependencies(t *testing.T) {
	testCases := []struct {
		name           string
		environment    map[string]any
		pythonVersion  string
		indexURL       string
		extraIndexUrls []string
		expectedResult map[string]any
	}{
		{
			name: "EmptyEnvironment",

			environment:   make(map[string]any),
			pythonVersion: "999.999.999",
			indexURL:      "https://example.com/index-url",
			extraIndexUrls: []string{
				"https://sub.example.com/extra-index-url",
				"https://sub2.example.com/extra-index-url",
			},

			expectedResult: map[string]any{
				"dependencies": []any{
					"python=999.999.999",
					"pip",
					"ipykernel",
					"nb_conda_kernels",
					"notebook",
					map[string][]any{
						"pip": {
							"--index-url https://example.com/index-url",
							"--extra-index-url https://sub.example.com/extra-index-url",
							"--extra-index-url https://sub2.example.com/extra-index-url",
							"--trusted-host example.com",
							"--trusted-host sub.example.com",
							"--trusted-host sub2.example.com",
						},
					},
				},
			},
		},
		{
			name: "EmptyEnvironmentWithoutIndexURL",

			environment:   make(map[string]any),
			pythonVersion: "999.999.999",
			indexURL:      "",
			extraIndexUrls: []string{
				"https://sub.example.com/extra-index-url",
				"https://sub2.example.com/extra-index-url",
			},

			expectedResult: map[string]any{
				"dependencies": []any{
					"python=999.999.999",
					"pip",
					"ipykernel",
					"nb_conda_kernels",
					"notebook",
					map[string][]any{
						"pip": {
							"--extra-index-url https://sub.example.com/extra-index-url",
							"--extra-index-url https://sub2.example.com/extra-index-url",
							"--trusted-host sub.example.com",
							"--trusted-host sub2.example.com",
						},
					},
				},
			},
		},
		{
			name: "EmptyEnvironmentWithoutExtraIndexURL",

			environment:    make(map[string]any),
			pythonVersion:  "999.999.999",
			indexURL:       "https://example.com/index-url",
			extraIndexUrls: []string{},

			expectedResult: map[string]any{
				"dependencies": []any{
					"python=999.999.999",
					"pip",
					"ipykernel",
					"nb_conda_kernels",
					"notebook",
					map[string][]any{
						"pip": {
							"--index-url https://example.com/index-url",
							"--trusted-host example.com",
						},
					},
				},
			},
		},
		{
			name: "EmptyEnvironmentWithoutIndexURLAndExtraIndexURL",

			environment:    make(map[string]any),
			pythonVersion:  "999.999.999",
			indexURL:       "",
			extraIndexUrls: []string{},

			expectedResult: map[string]any{
				"dependencies": []any{
					"python=999.999.999",
					"pip",
					"ipykernel",
					"nb_conda_kernels",
					"notebook",
				},
			},
		},
		{
			name: "EnvironmentWithIndexURLAndExtraIndexURL",

			environment: map[string]any{
				"dependencies": []any{
					"foo",
					"bar",
					"python=3.10.14=hb885b13_0",
					"pip=21.3.1",
					"baz",
				},
			},
			pythonVersion: "999.999.999",
			indexURL:      "https://example.com/index-url",
			extraIndexUrls: []string{
				"https://sub.example.com/extra-index-url",
				"https://sub2.example.com/extra-index-url",
			},

			expectedResult: map[string]any{
				"dependencies": []any{
					"foo",
					"bar",
					"baz",
					"python=999.999.999",
					"pip",
					"ipykernel",
					"nb_conda_kernels",
					"notebook",
					map[string][]any{
						"pip": {
							"--index-url https://example.com/index-url",
							"--extra-index-url https://sub.example.com/extra-index-url",
							"--extra-index-url https://sub2.example.com/extra-index-url",
							"--trusted-host example.com",
							"--trusted-host sub.example.com",
							"--trusted-host sub2.example.com",
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assignedEnvironment, err := assignEssentialDependencies(tc.environment, tc.pythonVersion, tc.indexURL, tc.extraIndexUrls)
			require.NoError(t, err)
			require.NotEmpty(t, assignedEnvironment)
			assert.Equal(t, tc.expectedResult, assignedEnvironment)
		})
	}
}

func TestSyncMissingBothFiles(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test-missing-both-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	condaLoader, err := NewCondaLoader(map[string]string{
		"name":                    "test-env",
		"condaEnvironmentYmlPath": tempDir + "/environment.yml",
		"pipRequirementsTxtPath":  tempDir + "/requirements.txt",
		"condaPrefixDir":          tempDir,
	}, Options{Root: tempDir + "/root"}, Secrets{})
	require.NoError(t, err)

	err = condaLoader.Sync("", "")
	require.Error(t, err)
}

func TestRenderPipConfig(t *testing.T) {
	pipConfig, err := renderPipConfig("https://example.com/index-url", []string{"https://sub.example.com/extra-index-url", "https://sub2.example.com/extra-index-url"})
	require.NoError(t, err)

	expected := `[global]
index-url = https://example.com/index-url
extra-index-url =
    https://sub.example.com/extra-index-url
    https://sub2.example.com/extra-index-url
trusted-host =
    example.com
    sub.example.com
    sub2.example.com
`

	assert.Equal(t, expected, pipConfig)

	pipConfig, err = renderPipConfig("", []string{"https://sub.example.com/extra-index-url", "https://sub2.example.com/extra-index-url"})
	require.NoError(t, err)

	expected = `[global]
extra-index-url =
    https://sub.example.com/extra-index-url
    https://sub2.example.com/extra-index-url
trusted-host =
    sub.example.com
    sub2.example.com
`

	assert.Equal(t, expected, pipConfig)

	pipConfig, err = renderPipConfig("", []string{"https://sub.example.com/extra-index-url"})
	require.NoError(t, err)

	expected = `[global]
extra-index-url = https://sub.example.com/extra-index-url
trusted-host = sub.example.com
`

	assert.Equal(t, expected, pipConfig)

	pipConfig, err = renderPipConfig("", []string{})
	require.NoError(t, err)

	expected = "[global]\n"

	assert.Equal(t, expected, pipConfig)
}

func TestSyncWithVenv(t *testing.T) {
	t.Run("pip only mode", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "test-pip-only-*")
		require.NoError(t, err)
		defer func() {
			assert.NoError(t, os.RemoveAll(tempDir))
		}()
		require.NoError(t, os.MkdirAll(tempDir+"/test-env/conda/envs", 0700))
		require.NoError(t, os.MkdirAll(tempDir+"/root", 0700))
		require.NoError(t, os.WriteFile(tempDir+"/requirements.txt", []byte("torch>=2.0\n"), 0600))

		condaLoader, err := NewCondaLoader(map[string]string{
			"name":                   "test-env",
			"pipIndexUrl":            "https://pypi.example.com/simple",
			"pipRequirementsTxtPath": tempDir + "/requirements.txt",
			"condaPrefixDir":         tempDir,
		}, Options{
			Root: tempDir + "/root",
		}, Secrets{})
		require.NoError(t, err)

		// Setup mocks
		fakePython := fakeCommand{t: t, cmd: "python3", outputs: []out{{exit: 0}}}
		defer func() { _ = fakePython.Clean() }()

		fakePip := fakeCommand{
			t: t, cmd: "pip",
			path:    path.Join(condaLoader.loaderOptions.envPrefix(), "bin"),
			outputs: []out{{stdout: "installed", exit: 0}},
		}
		defer func() { _ = fakePip.Clean() }()

		fakeRclone := fakeCommand{t: t, cmd: "rclone", outputs: []out{{exit: 0}}}
		defer func() { _ = fakeRclone.Clean() }()

		// Create venv directory structure
		venvPath := condaLoader.loaderOptions.envPrefix()
		require.NoError(t, os.MkdirAll(filepath.Join(venvPath, "bin"), 0700))

		fakePython.WithContext(func() {
			fakePip.WithContext(func() {
				fakeRclone.WithContext(func() {
					err = condaLoader.Sync("", "")
					assert.NoError(t, err)
				})
			})
		})

		// Verify commands were called correctly
		assert.Contains(t, string(fakePython.GetAllInputs()[0]), "-m venv")
		assert.Contains(t, string(fakePip.GetAllInputs()[0]), "install -r")
		assert.Contains(t, string(fakePip.GetAllInputs()[0]), tempDir+"/requirements.txt")

		rcloneInputs := fakeRclone.GetAllInputs()
		require.Len(t, rcloneInputs, 1)
		assert.Contains(t, string(rcloneInputs[0]), condaLoader.loaderOptions.prefixingEnvsDir)
		assert.Contains(t, string(rcloneInputs[0]), condaLoader.loaderOptions.finalEnvsDir)
		assert.Contains(t, string(rcloneInputs[0]), "--copy-links")

		info, err := os.Stat(condaLoader.loaderOptions.finalPkgsDir)
		require.NoError(t, err)
		require.True(t, info.IsDir())
	})
}
