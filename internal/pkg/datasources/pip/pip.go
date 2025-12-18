package pip

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/samber/lo"
	"github.com/sirupsen/logrus"

	"github.com/BaizeAI/dataset/pkg/utils"
)

type PipCLI struct {
	// Global
	// - In a “pip” subdirectory of any of the paths set in the environment variable XDG_CONFIG_DIRS (if it exists), for example /etc/xdg/pip/pip.conf.
	// This will be followed by loading /etc/pip.conf.
	//
	// User
	// - $HOME/.config/pip/pip.conf, which respects the XDG_CONFIG_HOME environment variable.
	// The legacy “per-user” configuration file is also loaded, if it exists: $HOME/.pip/pip.conf.
	//
	// Site
	// - $VIRTUAL_ENV/pip.conf
	// PIP_CONFIG_FILE
	// Additionally, the environment variable PIP_CONFIG_FILE can be used to specify a configuration file that’s loaded last, and whose values override
	// the values set in the aforementioned files. Setting this to os.devnull disables the loading of all configuration files. Note that if a file exists
	// at the location that this is set to, the user config file will not be loaded.
	//
	// Configuration - pip documentation v24.0 https://pip.pypa.io/en/stable/topics/configuration/
	ConfigFilePath string
	Bin            string
	EnvPath        string
}

func NewPipCLIWithCondaEnv(envPrefix string) *PipCLI {
	return &PipCLI{
		Bin:     filepath.Join(envPrefix, "bin", "pip"),
		EnvPath: filepath.Join(envPrefix, "bin"),
	}
}

// CreateVenv creates a virtual environment using python -m venv
func CreateVenv(logger *logrus.Entry, venvPath string) error {
	cmd := exec.Command("python3", "-m", "venv", venvPath) // #nosec G204
	cmd.Env = os.Environ()

	_, err := utils.ExecuteCommandWithOutput(logger, cmd, []string{})
	return err
}

func (p *PipCLI) bin() string {
	if p.Bin != "" {
		return p.Bin
	}

	return "pip"
}

// Equivalent to `pip --version`
func (p *PipCLI) Version(logger *logrus.Entry) (string, error) {
	args := []string{
		"--version",
	}

	cmd := exec.Command(p.bin(), args...) // #nosec G204
	cmd.Env = os.Environ()

	output, err := utils.ExecuteCommandWithOutput(logger, cmd, []string{})
	if err != nil {
		return "", err
	}

	outputString := strings.TrimSpace(output.String())
	output.Reset()

	return outputString, nil
}

// Equivalent to `pip install -r requirements.txt`
func (p *PipCLI) InstallWithRequirementsTxt(logger *logrus.Entry, requirementsTxt string) error {
	args := []string{
		"install",
		"-r",
		requirementsTxt,
	}

	cmd := exec.Command(p.bin(), args...) // #nosec G204
	cmd.Env = lo.Filter(os.Environ(), func(item string, index int) bool {
		return !strings.HasPrefix(item, "PATH=")
	})
	cmd.Env = append(cmd.Env, "PATH="+p.EnvPath+":"+os.Getenv("PATH"))

	if p.ConfigFilePath != "" {
		cmd.Env = append(cmd.Env, "PIP_CONFIG_FILE="+p.ConfigFilePath)
		logger = logger.WithField("PIP_CONFIG_FILE", p.ConfigFilePath)
	}

	_, err := utils.ExecuteCommandWithOutput(logger, cmd, []string{})
	if err != nil {
		return err
	}

	return nil
}
