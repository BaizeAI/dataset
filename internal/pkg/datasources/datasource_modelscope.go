package datasources

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/BaizeAI/dataset/pkg/log"
	"github.com/BaizeAI/dataset/pkg/utils"
)

var _ Loader = &ModelScopeLoader{}

type ModelScopeLoader struct {
	Options Options

	modelScopeOptions ModelScopeLoaderOptions
}

func NewModelScopeLoader(datasourceOptions map[string]string, options Options, secrets Secrets) (*ModelScopeLoader, error) {
	modelScope := new(ModelScopeLoader)
	parsedOpts, err := modelScope.parseOptionsFromOptions(datasourceOptions)
	if err != nil {
		return nil, err
	}

	modelScope.Options = options
	modelScope.modelScopeOptions = parsedOpts
	modelScope.modelScopeOptions.token = secrets.Token

	return modelScope, nil
}

type ModelScopeLoaderOptions struct {
	Revision string `json:"revision"`
	RepoType string `json:"repoType"`
	Include  string `json:"include"`
	Exclude  string `json:"exclude"`

	token string
}

func (d *ModelScopeLoader) parseOptionsFromOptions(options map[string]string) (ModelScopeLoaderOptions, error) {
	jsonContent, err := json.Marshal(options)
	if err != nil {
		return ModelScopeLoaderOptions{}, err
	}

	var msOptions ModelScopeLoaderOptions
	err = json.Unmarshal(jsonContent, &msOptions)
	if err != nil {
		return ModelScopeLoaderOptions{}, err
	}

	return msOptions, nil
}

func (d *ModelScopeLoader) mapRepoTypeEnumStringToModelScopeRepoType(repoType string) string {
	switch repoType {
	case "MODEL", "model":
		return "model"
	case "DATASET", "dataset":
		return "dataset"
	default:
		return ""
	}
}

func (d *ModelScopeLoader) login(logger *logrus.Entry, token string) error {
	args := []string{
		"login",
		"--token",
		token,
	}

	cmd := exec.Command("modelscope", args...)
	cmd.Env = os.Environ()

	_, err := utils.ExecuteCommandWithOutput(logger, cmd, []string{})
	if err != nil {
		return err
	}

	return nil
}

func (d *ModelScopeLoader) Sync(fromURI string, toPath string) error {
	parsedURL, err := url.Parse(d.Options.URI)
	if err != nil {
		return err
	}
	if parsedURL.Scheme != "modelscope" {
		return fmt.Errorf("invalid scheme %s, only modelscope is supported", parsedURL.Scheme)
	}

	repoName := parsedURL.Host + parsedURL.Path
	repoType := d.mapRepoTypeEnumStringToModelScopeRepoType(d.modelScopeOptions.RepoType)

	logger := log.WithFields(logrus.Fields{
		"fromURI":          fromURI,
		"type":             TypeModelScope,
		"toPath":           toPath,
		"workingDirectory": d.Options.Root,
		"repoName":         repoName,
		"revision":         d.modelScopeOptions.Revision,
		"repoType":         repoType,
		"include":          d.modelScopeOptions.Include,
		"exclude":          d.modelScopeOptions.Exclude,
	})

	token := strings.TrimSpace(d.modelScopeOptions.token)

	logger.Debugf("performing modelscope download command to pull data from %s to %s", fromURI, toPath)

	if d.modelScopeOptions.token != "" {
		err = d.login(logger, token)
		if err != nil {
			return err
		}
	}

	args := []string{
		"download",
		repoName,
		"--local_dir",
		toPath,
	}
	if repoType != "" {
		args = append(args, "--repo-type", repoType)
	}
	if d.modelScopeOptions.Include != "" {
		args = append(args, "--include", d.modelScopeOptions.Include)
	}
	if d.modelScopeOptions.Exclude != "" {
		args = append(args, "--exclude", d.modelScopeOptions.Exclude)
	}

	cmd := exec.Command("modelscope", args...)
	cmd.Dir = d.Options.Root

	logger = logger.WithField("command", cmd.String())
	logger.Debug("executing command to download data from modelscope")

	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "DO_NOT_TRACK=1") // https://consoledonottrack.com/

	outBuffer, errBuffer, err := utils.ExecuteCommandWithAllOutput(logger, cmd, []string{token})

	if err != nil {
		logger.Errorf("modelscope download command error: %s", errBuffer)
		return fmt.Errorf("failed to copy data from %s to %s with modelscope command %s, err: %s", fromURI, toPath, cmd.String(), err)
	}

	logger.Debugf("modelscope download command output: %s", outBuffer.String())

	return nil
}
