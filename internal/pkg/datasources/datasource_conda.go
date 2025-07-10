package datasources

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"

	"github.com/BaizeAI/dataset/internal/pkg/constants"
	"github.com/BaizeAI/dataset/internal/pkg/datasources/conda"
	"github.com/BaizeAI/dataset/internal/pkg/datasources/pip"
	"github.com/BaizeAI/dataset/pkg/log"
	"github.com/BaizeAI/dataset/pkg/utils"
)

type CondaLoaderOptions struct {
	Name                    string `json:"name"`
	PythonVersion           string `json:"pythonVersion"`
	PipIndexURL             string `json:"pipIndexUrl"`
	PipExtraIndexURL        string `json:"pipExtraIndexUrl"`
	CondaEnvironmentYmlPath string `json:"condaEnvironmentYmlPath"`
	PipRequirementsTxtPath  string `json:"pipRequirementsTxtPath"`
	CondaPrefixDir          string `json:"condaPrefixDir"`

	condaEnvironmentYml string
	pipRequirementsTxt  string

	prefixingPkgsDir string
	prefixingEnvsDir string

	finalPkgsDir string
	finalEnvsDir string
}

func (o *CondaLoaderOptions) parseOptionsFromOptions(rawOptions map[string]string, options Options) (CondaLoaderOptions, error) {
	jsonContent, err := json.Marshal(rawOptions)
	if err != nil {
		return CondaLoaderOptions{}, err
	}

	var loaderOptions CondaLoaderOptions
	err = json.Unmarshal(jsonContent, &loaderOptions)
	if err != nil {
		return CondaLoaderOptions{}, err
	}

	required := make([]string, 0, 3)
	if loaderOptions.Name == "" {
		required = append(required, "--options name=<env-name>")
	}
	if len(required) > 0 {
		return CondaLoaderOptions{}, fmt.Errorf("missing required options %s", strings.Join(required, ", "))
	}

	if loaderOptions.CondaEnvironmentYmlPath == "" {
		loaderOptions.CondaEnvironmentYmlPath = constants.DatasetJobCondaCondaEnvironmentYAMLPath
	}
	if loaderOptions.PipRequirementsTxtPath == "" {
		loaderOptions.PipRequirementsTxtPath = constants.DatasetJobCondaPipRequirementsTxtPath
	}
	if loaderOptions.CondaPrefixDir == "" {
		loaderOptions.CondaPrefixDir = constants.DatasetJobCondaMountDir
	}

	loaderOptions.prefixingPkgsDir = filepath.Join(loaderOptions.CondaPrefixDir, loaderOptions.Name, "conda", "pkgs")
	loaderOptions.prefixingEnvsDir = filepath.Join(loaderOptions.CondaPrefixDir, loaderOptions.Name, "conda", "envs")

	loaderOptions.finalPkgsDir = filepath.Join(options.Root, "conda", "pkgs")
	loaderOptions.finalEnvsDir = filepath.Join(options.Root, "conda", "envs")

	return loaderOptions, nil
}

func (o *CondaLoaderOptions) envPrefix() string {
	return filepath.Join(o.prefixingEnvsDir, o.Name)
}

func (o *CondaLoaderOptions) extraIndexURLs() []string {
	if o.PipExtraIndexURL == "" {
		return make([]string, 0)
	}

	return []string{o.PipExtraIndexURL}
}

var _ Loader = &CondaLoader{}

type CondaLoader struct {
	Options Options

	loaderOptions CondaLoaderOptions
	mamba         *conda.MambaCLI
	pip           *pip.PipCLI
}

func NewCondaLoader(datasourceOption map[string]string, options Options, secrets Secrets) (*CondaLoader, error) {
	loader := new(CondaLoader)
	loader.Options = options

	loaderOptions, err := loader.loaderOptions.parseOptionsFromOptions(datasourceOption, options)
	if err != nil {
		return nil, err
	}

	loader.loaderOptions = loaderOptions

	loader.mamba = conda.NewMambaCLI()
	loader.pip = pip.NewPipCLIWithCondaEnv(loader.loaderOptions.envPrefix())
	loader.tryReadFile()

	return loader, nil
}

func (l *CondaLoader) tryReadFile() {
	logger := log.WithFields(logrus.Fields{
		"condaEnvironmentYmlPath": l.loaderOptions.condaEnvironmentYml,
		"pipRequirementsTxtPath":  l.loaderOptions.PipRequirementsTxtPath,
	})

	condaEnvironmentYml, err := os.ReadFile(l.loaderOptions.CondaEnvironmentYmlPath)
	if err != nil {
		logger.WithError(err).Error("Failed to read conda environment yaml")
	}
	if err == nil {
		l.loaderOptions.condaEnvironmentYml = string(condaEnvironmentYml)
	}

	pipRequirementsTxt, err := os.ReadFile(l.loaderOptions.PipRequirementsTxtPath)
	if err != nil {
		logger.WithError(err).Error("Failed to read pip requirements txt")
	}
	if err == nil {
		l.loaderOptions.pipRequirementsTxt = string(pipRequirementsTxt)
	}
}

func (l *CondaLoader) writeTemp(logger *logrus.Entry, fileName string, content []byte) (string, func(), error) {
	tempDir, err := os.MkdirTemp("", "dataset-job-conda-env-*")
	if err != nil {
		return "", func() {}, err
	}

	cleanup := func() {
		err := os.RemoveAll(tempDir)
		if err != nil {
			logger.WithError(err).Error("Failed to remove temp dir")
		}
	}

	filePath := filepath.Join(tempDir, fileName)
	err = os.WriteFile(filePath, content, 0644) // #nosec G306
	if err != nil {
		return "", cleanup, err
	}

	return filePath, cleanup, nil
}

/*
Generally the environment.yaml may look like this:

	name: p310-test-1
	channels:
	- defaults
	- conda-forge
	dependencies:
	- bzip2=1.0.8=h80987f9_5
	- ca-certificates=2024.3.11=hca03da5_0
	- libffi=3.4.4=hca03da5_0
	- ncurses=6.4=h313beb8_0
	- openssl=3.0.13=h1a28f6b_0
	- pip=23.3.1=py310hca03da5_0
	- python=3.10.14=hb885b13_0
	- readline=8.2=h1a28f6b_0
	- setuptools=68.2.2=py310hca03da5_0
	- sqlite=3.41.2=h80987f9_0
	- tk=8.6.12=hb8d0fd4_0
	- tzdata=2024a=h04d1e81_0
	- wheel=0.41.2=py310hca03da5_0
	- xz=5.4.6=h80987f9_0
	- zlib=1.2.13=h5a0b063_0
	prefix: /opt/baize-runtime-env/conda/envs/p310-test-1

1. the name will be altered to the env name, since it is required to be aligned with the dataset name.

2. the prefix will be alter to the new env name.

3. python=3.10.14=hb885b13_0 and pip=23.3.1=py310hca03da5_0 will be removed, since we will install python
and pip separately with the specified version passed by pythonVersion
*/
func normalizeEnvironmentYaml(
	environment map[string]any,
	name,
	pythonVersion,
	pipIndexURL string,
	pipExtraIndexURL []string,
	prefix string,
) (map[string]any, error) {
	if environment == nil {
		environment = make(map[string]any)
	}

	// alter name
	environment["name"] = name
	// alter prefix
	environment["prefix"] = prefix
	// configure default channels
	_, ok := environment["channels"]
	if !ok {
		environment["channels"] = []any{"defaults", "conda-forge"}
	}

	environment["default_threads"] = 4

	// remove python=* and pip=* and assign python and pip with specified version and pipIndexURL
	return assignEssentialDependencies(environment, pythonVersion, pipIndexURL, pipExtraIndexURL)
}

// In order to persist the --extra-index-url in the environment.yml, and allow users to use and share the
// same configurations across different environments (jobs, notebooks, etc.), we need to assign the
// --extra-index-url to the pip in the dependencies array.
//
// This is a advanced usage of environment.yml to configure pip.
//
// References:
//
// conda/tests/env/support/advanced-pip/environment.yml at main · conda/conda
// https://github.com/conda/conda/blob/main/tests/env/support/advanced-pip/environment.yml
//
// python - How to specify pip --extra-index-url in environment.yml? - Stack Overflow
// https://stackoverflow.com/questions/73287475/how-to-specify-pip-extra-index-url-in-environment-yml
func assignPipArgumentsIfHaveAny(dependencies []any, pipIndexURL string, pipExtraIndexURLs []string) ([]any, error) {
	// 1 reserved arguments for --index-url
	// multiplied by 2 for --trusted-host diverged from --index-url and --extra-index-url
	// therefore: [(--index-url), (--trusted-host), (--extra-index-url), (--trusted-host), (--extra-index-url), (--trusted-host)...]
	pipArguments := make([]string, 0, (1+len(pipExtraIndexURLs))*2)
	// 1 reserved arguments for --index-url
	trustedHosts := make([]string, 0, 1+len(pipExtraIndexURLs))

	pipExtraIndexURLs = lo.Filter(pipExtraIndexURLs, func(extraIndexURL string, _ int) bool {
		return extraIndexURL != ""
	})

	if pipIndexURL != "" {
		parsedPipIndexURL, err := url.Parse(pipIndexURL)
		if err != nil {
			return nil, err
		}

		pipArguments = append(pipArguments, exec.Command("--index-url", pipIndexURL).String())
		trustedHosts = append(trustedHosts, parsedPipIndexURL.Host)
	}
	if len(pipExtraIndexURLs) > 0 {
		for _, extraIndexURL := range pipExtraIndexURLs {
			parsedExtraIndexURL, err := url.Parse(extraIndexURL)
			if err != nil {
				return nil, err
			}

			pipArguments = append(pipArguments, exec.Command("--extra-index-url", extraIndexURL).String())
			trustedHosts = append(trustedHosts, parsedExtraIndexURL.Host)
		}
	}

	pipArguments = append(pipArguments, lo.Map(trustedHosts, func(trustedHost string, _ int) string {
		return exec.Command("--trusted-host", trustedHost).String()
	})...)

	if len(pipArguments) == 0 {
		return dependencies, nil
	}

	return append(dependencies, map[string][]any{
		"pip": lo.Map(pipArguments, func(pipArgument string, _ int) any {
			return pipArgument
		}),
	}), nil
}

// subtle utility function to assign python and pip with specified version and pipIndexURL
func assignEssentialDependencies(environment map[string]any, pythonVersion, pipIndexURL string, pipExtraIndexURL []string) (map[string]any, error) {
	defaultDependenciesItems := []any{
		"python=" + pythonVersion, // required
		"pip",                     // required
		"ipykernel",               //  to allow the kernel to be used in Jupyter Notebook
		"nb_conda_kernels",        // to allow the kernel to be used in Jupyter Notebook
		"notebook",                // TODO: workaround for https://github.com/anaconda/nb_conda_kernels/issues/280
	}

	// have dependencies?
	dependenciesRaw, ok := environment["dependencies"]
	if !ok {
		// fallback to default dependencies
		dependencies := defaultDependenciesItems
		dependencies, err := assignPipArgumentsIfHaveAny(dependencies, pipIndexURL, pipExtraIndexURL)
		if err != nil {
			return nil, err
		}

		environment["dependencies"] = dependencies

		return environment, nil
	}

	// assert dependencies is an array
	dependencies, assertOk := dependenciesRaw.([]any)
	if !assertOk {
		return nil, fmt.Errorf("dependencies in environment.yaml is not an array")
	}

	// remove python=*, pip=*, and ipykernel=*
	dependencies = lo.Filter(dependencies, func(dep any, _ int) bool {
		depStr, ok := dep.(string)
		if !ok {
			return true
		}

		return !strings.HasPrefix(depStr, "python=") &&
			!strings.HasPrefix(depStr, "pip=") &&
			!strings.HasPrefix(depStr, "ipykernel=") &&
			!strings.HasPrefix(depStr, "nb_conda_kernels=")
	})

	dependencies = append(dependencies, defaultDependenciesItems...)
	dependencies, err := assignPipArgumentsIfHaveAny(dependencies, pipIndexURL, pipExtraIndexURL)
	if err != nil {
		return nil, err
	}

	environment["dependencies"] = dependencies

	return environment, nil
}

/*
Render pip.conf

	`[global]
	index-url = https://example.com/index-url
	extra-index-url =
		https://sub.example.com/extra-index-url
		https://sub2.example.com/extra-index-url
	trusted-host =
		mirror1.example.com
		mirror2.example.com`

About pip.conf: https://pip.pypa.io/en/stable/topics/configuration/#configuration
About repeatable options: https://pip.pypa.io/en/stable/topics/configuration/#repeatable-options
*/
func renderPipConfig(pipIndexURL string, pipExtraIndexURLs []string) (string, error) {
	pipExtraIndexURLs = lo.Filter(pipExtraIndexURLs, func(extraIndexURL string, _ int) bool {
		return extraIndexURL != ""
	})

	var sb strings.Builder
	sb.WriteString("[global]\n")
	if pipIndexURL == "" && len(pipExtraIndexURLs) == 0 {
		return sb.String(), nil
	}

	trustedHosts := make([]string, 0, 2)

	if pipIndexURL != "" {
		parsedPipIndexURL, err := url.Parse(pipIndexURL)
		if err != nil {
			return "", err
		}

		sb.WriteString(fmt.Sprintf("index-url = %s\n", pipIndexURL))
		trustedHosts = append(trustedHosts, parsedPipIndexURL.Host)
	}
	if len(pipExtraIndexURLs) > 0 {
		if len(pipExtraIndexURLs) == 1 {
			parsedExtraIndexURL, err := url.Parse(pipExtraIndexURLs[0])
			if err != nil {
				return "", err
			}

			sb.WriteString(fmt.Sprintf("extra-index-url = %s\n", pipExtraIndexURLs[0]))
			trustedHosts = append(trustedHosts, parsedExtraIndexURL.Host)
		} else {
			extraIndexURLs := make([]string, 0, len(pipExtraIndexURLs))
			for _, extraIndexURL := range pipExtraIndexURLs {
				parsedExtraIndexURL, err := url.Parse(extraIndexURL)
				if err != nil {
					return "", err
				}

				extraIndexURLs = append(extraIndexURLs, extraIndexURL)
				trustedHosts = append(trustedHosts, parsedExtraIndexURL.Host)
			}

			sb.WriteString(fmt.Sprintf("extra-index-url =\n    %s\n", strings.Join(extraIndexURLs, "\n    ")))
		}
	}
	if len(trustedHosts) > 0 {
		if len(trustedHosts) == 1 {
			sb.WriteString(fmt.Sprintf("trusted-host = %s\n", trustedHosts[0]))
		} else {
			sb.WriteString(fmt.Sprintf("trusted-host =\n    %s\n", strings.Join(trustedHosts, "\n    ")))
		}
	}

	return sb.String(), nil
}

func (l *CondaLoader) cleanupNonExistingSymlinks(logger *logrus.Entry) error {
	// The reason why we need to clean up non-existing symlinks is that the rclone may fail to copy the symlinks
	// if the target file does not exist. For example, the following error may occur:
	//
	// python-3.10.0-h12debd9_5/compiler_compat/ld: Listing error: symlink: stat /opt/baize-runtime-env/pytorch/conda/pkgs/python-3.10.0-h12debd9_5/compiler_compat/ld: no such file or directory
	// Failed to copyto: symlink: stat /opt/baize-runtime-env/pytorch/conda/pkgs/python-3.10.0-h12debd9_5/compiler_compat/ld: no such file or directory
	// failed to load data: failed to execute command /usr/local/bin/rclone copyto /opt/baize-runtime-env/pytorch/conda/pkgs /baize/dataset/data/conda/pkgs --copy-links, err: exit status 6
	//
	// while this is not a critical error, it is better to clean up the non-existing symlinks to avoid the error,
	// as well as better error handling.
	err := utils.CleanupNotExistingSymlinks(logger, l.loaderOptions.prefixingEnvsDir)
	if err != nil {
		logger.WithError(err).Error("Failed to cleanup not existing symlinks")
		return err
	}

	err = utils.CleanupNotExistingSymlinks(logger, l.loaderOptions.prefixingPkgsDir)
	if err != nil {
		logger.WithError(err).Error("Failed to cleanup not existing symlinks")
		return err
	}

	return nil
}

func (l *CondaLoader) moveToMountRoot(logger *logrus.Entry) error {
	err := os.MkdirAll(filepath.Dir(l.loaderOptions.finalPkgsDir), 0755)
	if err != nil {
		logger.WithError(err).Error("Failed to create conda dir")
		return err
	}

	err = os.RemoveAll(l.loaderOptions.finalPkgsDir)
	if err != nil {
		logger.WithError(err).Error("Failed to remove conda pkgs dir")
		return err
	}

	err = os.RemoveAll(l.loaderOptions.finalEnvsDir)
	if err != nil {
		logger.WithError(err).Error("Failed to remove conda envs dir")
		return err
	}

	cmd := exec.Command("rclone",
		"copyto",
		l.loaderOptions.prefixingPkgsDir,
		l.loaderOptions.finalPkgsDir,
		"--copy-links",
	) // #nosec G204

	err = utils.ExecuteCommand(logger, cmd, []string{})
	if err != nil {
		logger.WithError(err).Error("Failed to move conda pkgs to mount root")
		return err
	}

	cmd = exec.Command("rclone",
		"copyto",
		l.loaderOptions.prefixingEnvsDir,
		l.loaderOptions.finalEnvsDir,
		"--copy-links",
	) // #nosec G204

	err = utils.ExecuteCommand(logger, cmd, []string{})
	if err != nil {
		logger.WithError(err).Error("Failed to move conda envs to mount root")
		return err
	}

	return nil
}

// Workflow overview:
//   - conda --version
//   - conda info
//   - conda env list
//   - conda config --show-sources
//   - conda config --set show_channel_urls yes
//   - conda config --prepend pkgs_dirs /opt/baize-runtime-env/conda/pkgs
//   - conda config --prepend envs_dirs /opt/baize-runtime-env/conda/envs
//
// if environment.yml exists:
//   - read environment.yml
//   - normalize environment.yml
//   - conda env create -f environment.yml
//   - conda clean --all
//
// else:
//   - create a normalized environment.yml
//   - conda env create -f environment.yml
//   - conda clean --all
//
// if requirements.txt exists:
//   - pip install -r requirements.txt
//
// finalize the conda environment:
//   - mv /opt/baize-runtime-env/conda/pkgs ${mount-root}/conda/pkgs
//   - mv /opt/baize-runtime-env/conda/envs ${mount-root}/conda/envs
func (l *CondaLoader) Sync(_ string, _ string) error {
	logger := log.WithFields(logrus.Fields{
		"type":                        TypeConda,
		"applicationWorkingDirectory": lo.Must(os.Getwd()),
		"root":                        l.Options.Root,
		"envName":                     l.loaderOptions.Name,
	})

	// Check if conda is installed
	condaVersion, err := l.mamba.Version(logger)
	if err != nil {
		logger.WithError(err).Error("Failed to get conda version")
		return err
	}

	logger.WithField("condaVersion", condaVersion).Info("Conda version")

	// Configure conda
	{
		// Configure conda show channel URLs
		l.mamba.ConfigSetShowChannelURLs(logger)
		// Configure conda pkgs dir
		l.mamba.ConfigPrependPkgsDir(logger, l.loaderOptions.prefixingPkgsDir)
		// Configure conda envs dir
		l.mamba.ConfigPrependEnvsDir(logger, l.loaderOptions.prefixingEnvsDir)
	}

	// Get conda info
	_, err = l.mamba.Info(logger)
	if err != nil {
		logger.WithError(err).Error("Failed to get conda info")
		return err
	}

	// Check env lists before creating and configuring
	_, err = l.mamba.EnvList(logger)
	if err != nil {
		logger.WithError(err).Error("Failed to get conda env list")
		return err
	}

	var environment map[string]any

	if l.loaderOptions.condaEnvironmentYml != "" {
		err = yaml.Unmarshal([]byte(l.loaderOptions.condaEnvironmentYml), &environment)
		if err != nil {
			logger.WithError(err).Error("Failed to unmarshal environment")
			return err
		}

		fmt.Printf("loaded environment:\n%s\n", lo.Must(yaml.Marshal(environment)))
	} else {
		environment = make(map[string]any)
	}

	environment, err = normalizeEnvironmentYaml(
		environment,
		l.loaderOptions.Name,
		l.loaderOptions.PythonVersion,
		l.loaderOptions.PipIndexURL,
		l.loaderOptions.extraIndexURLs(),
		l.loaderOptions.envPrefix(),
	)
	if err != nil {
		logger.WithError(err).Error("Failed to normalize environment")
		return err
	}

	environmentYAMLData, err := yaml.Marshal(environment)
	if err != nil {
		logger.WithError(err).Error("Failed to marshal environment")
		return err
	}

	fmt.Printf("new modified environment:\n%s\n", lo.Must(yaml.Marshal(environment)))

	environmentFilePath, cleanup, err := l.writeTemp(logger, "environment.yml", environmentYAMLData)
	if err != nil {
		logger.WithError(err).Error("Failed to write temp environment file")
		return err
	}
	defer cleanup()

	err = l.mamba.CreateEnvFromFile(logger, environmentFilePath)
	if err != nil {
		logger.WithError(err).Error("Failed to create conda env from file")
		return err
	}

	if l.loaderOptions.pipRequirementsTxt != "" {
		if l.loaderOptions.PipIndexURL != "" || l.loaderOptions.PipExtraIndexURL != "" {
			pipConfig, err := renderPipConfig(
				l.loaderOptions.PipIndexURL,
				l.loaderOptions.extraIndexURLs(),
			)
			if err != nil {
				logger.WithError(err).Error("Failed to render pip config")
				return err
			}

			pipConfigFilePath, cleanup, err := l.writeTemp(logger, "pip.conf", []byte(pipConfig))
			if err != nil {
				logger.WithError(err).Error("Failed to write temp pip config file")
				return err
			}
			defer cleanup()

			l.pip.ConfigFilePath = pipConfigFilePath
		}

		requirementsFilePath, cleanup, err := l.writeTemp(logger, "requirements.txt", []byte(l.loaderOptions.pipRequirementsTxt))
		if err != nil {
			logger.WithError(err).Error("Failed to write temp requirements file")
			return err
		}
		defer cleanup()

		// Install requirements
		err = l.pip.InstallWithRequirementsTxt(logger, requirementsFilePath)
		if err != nil {
			logger.WithError(err).Error("Failed to install requirements")
			return err
		}
	}

	err = l.mamba.CleanAll(logger)
	if err != nil {
		logger.WithError(err).Error("Failed to cleanup all packages, index cache, and tarballs, etc.")
		return err
	}

	err = l.cleanupNonExistingSymlinks(logger)
	if err != nil {
		logger.WithError(err).Error("Failed to cleanup non-existing symlinks")
		return err
	}

	err = l.moveToMountRoot(logger)
	if err != nil {
		logger.WithError(err).Error("Failed to move conda envs and pkgs to mount root")
		return err
	}

	return nil
}
