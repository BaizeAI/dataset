package datasources

import (
	"encoding/json"
	"fmt"
	"github.com/samber/lo"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/BaizeAI/dataset/pkg/log"
	"github.com/BaizeAI/dataset/pkg/utils"
)

var _ Loader = &S3Loader{}

type S3Loader struct {
	Options Options

	s3Options S3LoaderOptions
}

func NewS3Loader(datasourceOptions map[string]string, options Options, secrets Secrets) (*S3Loader, error) {
	s3 := new(S3Loader)
	s3Options, err := s3.parseOptionsFromOptions(datasourceOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to parse uri %s: %w", options.URI, err)
	}

	s3.Options = options
	s3.s3Options = s3Options
	s3.s3Options.accessKeyID = secrets.AKSKAccessKeyID
	s3.s3Options.secretAccessKey = secrets.AKSKSecretAccessKey
	s3.s3Options.SyncMode = lo.CoalesceOrEmpty(datasourceOptions["syncMode"], "sync")

	err = s3.validateOptions(s3Options)
	if err != nil {
		return nil, err
	}

	return s3, nil
}

type S3LoaderOptions struct {
	Provider string `json:"provider"`
	Region   string `json:"region"`
	Endpoint string `json:"endpoint"`
	SyncMode string `json:"syncMode"`

	accessKeyID     string
	secretAccessKey string
}

func (d *S3Loader) parseOptionsFromOptions(options map[string]string) (S3LoaderOptions, error) {
	jsonContent, err := json.Marshal(options)
	if err != nil {
		return S3LoaderOptions{}, err
	}

	var s3Options S3LoaderOptions
	err = json.Unmarshal(jsonContent, &s3Options)
	if err != nil {
		return S3LoaderOptions{}, err
	}

	return s3Options, nil
}

func (d *S3Loader) validateOptions(options S3LoaderOptions) error {
	if options.Provider == "AWS" && options.Region == "" {
		return fmt.Errorf("--options region <region> is required for AWS provider")
	}

	if options.SyncMode != "" && options.SyncMode != "sync" && options.SyncMode != "copy" {
		return fmt.Errorf("invalid syncMode '%s', must be 'sync' or 'copy'", options.SyncMode)
	}

	return nil
}

func (d *S3Loader) mapProviderEnumStringToRCloneProvider(provider string) string {
	switch provider {
	case "AWS":
		return "AWS"
	case "MINIO":
		return "Minio"
	default:
		return ""
	}
}

func (d *S3Loader) configTouch() error {
	cmd := exec.Command("rclone", "config", "touch")

	logger := log.WithField("command", cmd.String())
	logger.Debug("executing command to touch rclone config")

	outBuffer, errWriter, err := utils.ExecuteCommandWithAllOutput(logger, cmd, nil)
	if err != nil {
		logger.Errorf("rclone config touch command error: %s", errWriter)
		return err
	}
	logger.Debugf("rclone config touch command output: %s", outBuffer.String())

	return nil
}

func (d *S3Loader) configCreate(configName string) error {
	logger := log.WithFields(logrus.Fields{
		"configName": configName,
		"type":       TypeS3,
		"provider":   d.s3Options.Provider,
		"region":     d.s3Options.Region,
		"endpoint":   d.s3Options.Endpoint,
	})

	args := []string{
		"config",
		"create",
		configName,
		"s3",
	}
	if d.s3Options.accessKeyID != "" {
		args = append(args, strings.Join([]string{"env_auth", "true"}, "="))
	}
	if d.s3Options.Region != "" {
		args = append(args, strings.Join([]string{"region", d.s3Options.Region}, "="))
	}

	provider := d.mapProviderEnumStringToRCloneProvider(d.s3Options.Provider)

	if d.s3Options.Provider != "" {
		args = append(args, strings.Join([]string{"provider", provider}, "="))
	} else {
		args = append(args, strings.Join([]string{"provider", "AWS"}, "="))
	}

	if d.s3Options.Region == "" && (provider == "" || provider == "AWS") {
		// fallback as default region
		d.s3Options.Region = "us-east-1"
	}

	if d.s3Options.Endpoint != "" {
		args = append(args, strings.Join([]string{"endpoint", d.s3Options.Endpoint}, "="))
	}

	cmd := exec.Command("rclone", args...)

	logger = logger.WithField("command", cmd.String())
	logger.Debug("executing command to create a new rclone config")

	outBuffer, errBuffer, err := utils.ExecuteCommandWithAllOutput(logger, cmd, nil)

	if err != nil {
		logger.Errorf("rclone config create command error: %s", errBuffer)
		return err
	}
	logger.Debugf("rclone config create command output: %s", outBuffer.String())

	return nil
}

func (d *S3Loader) Sync(fromURI string, toPath string) error {
	parsedURL, err := url.Parse(d.Options.URI)
	if err != nil {
		return err
	}
	if parsedURL.Scheme != "s3" {
		return fmt.Errorf("invalid scheme %s, only s3 is supported", parsedURL.Scheme)
	}

	bucket := parsedURL.Host
	objectDir := strings.TrimPrefix(parsedURL.Path, "/")

	logger := log.WithFields(logrus.Fields{
		"fromURI":          fromURI,
		"type":             TypeS3,
		"toPath":           toPath,
		"workingDirectory": d.Options.Root,
		"provider":         d.s3Options.Provider,
		"region":           d.s3Options.Region,
		"endpoint":         d.s3Options.Endpoint,
		"bucket":           bucket,
		"objectDir":        objectDir,
	})

	accessKeyID := strings.TrimSpace(d.s3Options.accessKeyID)
	secretAccessKey := strings.TrimSpace(d.s3Options.secretAccessKey)

	logger.Debugf("performing rclone copy command to copy data served by S3")

	err = d.configTouch()
	if err != nil {
		return err
	}

	configName := fmt.Sprintf("baize-data-loader-copy-config-%s", utils.RandomHashString(8))

	err = d.configCreate(configName)
	if err != nil {
		return err
	}

	syncMode := d.s3Options.SyncMode

	args := []string{
		syncMode,
		filepath.Join(fmt.Sprintf("%s:%s", configName, bucket), objectDir),
		toPath,
	}

	args = append(args, "-vvv")
	cmd := exec.Command("rclone", args...)
	cmd.Dir = d.Options.Root

	logger = logger.WithField("command", cmd.String())
	logger.Debug("executing command to copy data")

	cmd.Env = os.Environ()

	if accessKeyID != "" && secretAccessKey != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("RCLONE_S3_ACCESS_KEY_ID=%s", accessKeyID))
		cmd.Env = append(cmd.Env, fmt.Sprintf("RCLONE_S3_SECRET_ACCESS_KEY=%s", secretAccessKey))
	}

	outBuffer, errBuffer, err := utils.ExecuteCommandWithAllOutput(logger, cmd, []string{accessKeyID, secretAccessKey})

	if err != nil {
		logger.Errorf("rclone copy command error: %s", errBuffer)
		return fmt.Errorf("failed to copy data from %s to %s with rclone command %s, err: %s", fromURI, toPath, cmd.String(), err)
	}
	logger.Debugf("rclone copy command output: %s", outBuffer.String())

	return nil
}
