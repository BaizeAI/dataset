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

var _ Loader = &HuggingFaceLoader{}

type HuggingFaceLoader struct {
	Options Options

	huggingFaceOptions HuggingFaceLoaderOptions
}

func NewHuggingFaceLoader(datasourceOptions map[string]string, options Options, secrets Secrets) (*HuggingFaceLoader, error) {
	huggingFace := new(HuggingFaceLoader)
	parsedOpts, err := huggingFace.parseOptionsFromOptions(datasourceOptions)
	if err != nil {
		return nil, err
	}

	huggingFace.Options = options
	huggingFace.huggingFaceOptions = parsedOpts
	huggingFace.huggingFaceOptions.token = secrets.Token

	err = huggingFace.validateOptions(parsedOpts)
	if err != nil {
		return nil, err
	}

	return huggingFace, nil
}

type HuggingFaceLoaderOptions struct {
	Revision string `json:"revision"`
	RepoType string `json:"repoType"`
	Endpoint string `json:"endpoint"`
	Offline  bool   `json:"offline"`
	Include  string `json:"include"`
	Exclude  string `json:"exclude"`

	token string
}

func (d *HuggingFaceLoader) parseOptionsFromOptions(options map[string]string) (HuggingFaceLoaderOptions, error) {
	jsonContent, err := json.Marshal(options)
	if err != nil {
		return HuggingFaceLoaderOptions{}, err
	}

	var hfOptions HuggingFaceLoaderOptions
	err = json.Unmarshal(jsonContent, &hfOptions)
	if err != nil {
		return HuggingFaceLoaderOptions{}, err
	}

	return hfOptions, nil
}

func (d *HuggingFaceLoader) validateOptions(options HuggingFaceLoaderOptions) error {
	if options.Endpoint != "" {
		_, err := url.Parse(options.Endpoint)
		if err != nil {
			return fmt.Errorf("invalid endpoint %s: %w", options.Endpoint, err)
		}
	}

	return nil
}

func (d *HuggingFaceLoader) mapRepoTypeEnumStringToHuggingFaceRepoType(repoType string) string {
	switch repoType {
	case "MODEL", "model":
		return "model"
	case "DATASET", "dataset":
		return "dataset"
	default:
		return ""
	}
}

func (d *HuggingFaceLoader) env(logger *logrus.Entry) (string, error) {
	args := []string{
		"env",
	}

	cmd := exec.Command("huggingface-cli", args...)
	cmd.Env = os.Environ()

	output, err := utils.ExecuteCommandWithOutput(logger, cmd, []string{})
	if err != nil {
		return "", err
	}

	outputString := strings.TrimSpace(output.String())
	output.Reset()

	return outputString, nil
}

func (d *HuggingFaceLoader) login(logger *logrus.Entry, token string) error {
	args := []string{
		"login",
		"--token",
		token,
	}

	cmd := exec.Command("huggingface-cli", args...)
	cmd.Env = os.Environ()

	_, err := utils.ExecuteCommandWithOutput(logger, cmd, []string{})
	if err != nil {
		return err
	}

	return nil
}

func (d *HuggingFaceLoader) whoAmI(logger *logrus.Entry) (string, error) {
	args := []string{
		"whoami",
	}

	cmd := exec.Command("huggingface-cli", args...)
	cmd.Env = os.Environ()

	output, err := utils.ExecuteCommandWithOutput(logger, cmd, []string{})
	if err != nil {
		return "", err
	}

	outputString := strings.TrimSpace(output.String())
	output.Reset()

	return outputString, nil
}

func (d *HuggingFaceLoader) Sync(fromURI string, toPath string) error {
	parsedURL, err := url.Parse(d.Options.URI)
	if err != nil {
		return err
	}
	if parsedURL.Scheme != "huggingface" {
		return fmt.Errorf("invalid scheme %s, only huggingface is supported", parsedURL.Scheme)
	}

	repoName := parsedURL.Host + parsedURL.Path
	repoType := d.mapRepoTypeEnumStringToHuggingFaceRepoType(d.huggingFaceOptions.RepoType)

	logger := log.WithFields(logrus.Fields{
		"fromURI":          fromURI,
		"type":             TypeHuggingFace,
		"toPath":           toPath,
		"workingDirectory": d.Options.Root,
		"repoName":         repoName,
		"revision":         d.huggingFaceOptions.Revision,
		"repoType":         repoType,
		"endpoint":         d.huggingFaceOptions.Endpoint,
		"offline":          d.huggingFaceOptions.Offline,
		"include":          d.huggingFaceOptions.Include,
		"exclude":          d.huggingFaceOptions.Exclude,
	})

	token := strings.TrimSpace(d.huggingFaceOptions.token)

	logger.Debugf("performing huggingface-cli download command to pull data from %s to %s", fromURI, toPath)

	_, err = d.env(logger)
	if err != nil {
		return err
	}

	if d.huggingFaceOptions.token != "" {
		err = d.login(logger, token)
		if err != nil {
			return err
		}

		whoAmI, err := d.whoAmI(logger)
		if err != nil {
			return err
		}

		logger.Debugf("huggingface-cli executed with authorized login handle as: %s", whoAmI)
	}

	args := []string{
		"download",
		repoName,
		"--local-dir",
		toPath,
		"--resume-download",
	}
	if repoType != "" {
		args = append(args, "--repo-type", repoType)
	}
	if d.huggingFaceOptions.Include != "" {
		args = append(args, "--include", d.huggingFaceOptions.Include)
	}
	if d.huggingFaceOptions.Exclude != "" {
		args = append(args, "--exclude", d.huggingFaceOptions.Exclude)
	}

	cmd := exec.Command("huggingface-cli", args...)
	cmd.Dir = d.Options.Root

	logger = logger.WithField("command", cmd.String())
	logger.Debug("executing command to download data from huggingface-cli")

	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "HF_HUB_VERBOSITY=debug")
	// cmd.Env = append(cmd.Env, "HF_HUB_DISABLE_PROGRESS_BARS=1")
	cmd.Env = append(cmd.Env, "HF_HUB_DOWNLOAD_TIMEOUT=60")
	cmd.Env = append(cmd.Env, "DO_NOT_TRACK=1") // https://consoledonottrack.com/

	if d.huggingFaceOptions.Offline {
		cmd.Env = append(cmd.Env, "HF_HUB_OFFLINE=1")
	}
	if d.huggingFaceOptions.Endpoint != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("HF_ENDPOINT=%s", d.huggingFaceOptions.Endpoint))
	}

	outBuffer, errBuffer, err := utils.ExecuteCommandWithAllOutput(logger, cmd, []string{token})

	if err != nil {
		logger.Errorf("huggingface-cli download command error: %s", errBuffer)
		return fmt.Errorf("failed to copy data from %s to %s with huggingface-cli command %s, err: %s", fromURI, toPath, cmd.String(), err)
	}
	logger.Debugf("huggingface-cli download command output: %s", outBuffer.String())

	return nil
}
