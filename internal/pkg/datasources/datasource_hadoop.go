package datasources

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"

	"github.com/BaizeAI/dataset/pkg/log"

	"github.com/sirupsen/logrus"
)

var _ Loader = &ModelHadoopLoader{}

type ModelHadoopLoader struct {
	Options Options

	modelHadoopOptions ModelHadoopLoaderOptions
}

func NewModelHadoopLoader(datasourceOptions map[string]string, options Options, secrets Secrets) (*ModelHadoopLoader, error) {
	res := new(ModelHadoopLoader)
	parsedOpts, err := res.convertHadoopOptions(datasourceOptions)
	if err != nil {
		return nil, err
	}

	res.Options = options
	res.modelHadoopOptions = parsedOpts
	return res, nil
}

type ModelHadoopLoaderOptions struct {
	SourcePath string `json:"sourcePath"`
}

func (d *ModelHadoopLoader) convertHadoopOptions(options map[string]string) (ModelHadoopLoaderOptions, error) {
	var hadoopOptions ModelHadoopLoaderOptions
	jsonContent, err := json.Marshal(options)
	if err != nil {
		return ModelHadoopLoaderOptions{}, err
	}
	err = json.Unmarshal(jsonContent, &hadoopOptions)
	if err != nil {
		return ModelHadoopLoaderOptions{}, err
	}
	if hadoopOptions.SourcePath == "" {
		return ModelHadoopLoaderOptions{}, fmt.Errorf("sourcePath option is required and must not be empty")
	}
	return hadoopOptions, nil
}

func (d *ModelHadoopLoader) Sync(fromURI string, toPath string) error {
	parsedURL, err := url.Parse(d.Options.URI)
	if err != nil {
		return err
	}
	if parsedURL.Scheme != "hdfs" {
		return fmt.Errorf("invalid scheme %s, only hdfs is supported", parsedURL.Scheme)
	}

	logger := log.WithFields(logrus.Fields{
		"fromURI":          fromURI,
		"type":             TypeHadoop,
		"toPath":           toPath,
		"workingDirectory": d.Options.Root,
		"sourcePath":       d.modelHadoopOptions.SourcePath,
	})
	// Adding "--" is to prevent injection, and when overwriting a file, the absence of the "-f" option will be treated as a failure.
	// #nosec G204
	cmd := exec.Command("hdfs", "dfs", "-get", "-f", "--", d.modelHadoopOptions.SourcePath, d.Options.Root)
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		logger.Errorf("running command failed: %v", err)
		return fmt.Errorf("%v: %s", err, stderr.String())
	}
	logger.Infof("get data from hdfs success, command output: %s", out.String())
	return nil
}
