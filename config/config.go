package config

import (
	"os"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

var (
	config *configuration
)

type configuration struct {
	DatasetJobSpecYaml      string `json:"dataset_job_spec_yaml"`
	EnableCascadingDeletion bool   `json:"enable_cascading_deletion"`
}

func GetDatasetJobSpecYaml() string {
	if config == nil || config.DatasetJobSpecYaml == "" {
		return `
backoffLimit: 4
completionMode: NonIndexed
completions: 1
parallelism: 1
template:
  spec:
    restartPolicy: Never
    containers:
    - image: ubuntu:20.04
      command: ["/bin/bash", "-c", "echo 'Container args: '$(echo $@)"]
      #command: ["/bin/bash", "-c", "--"]
      resources:
        requests:
          cpu: 100m
          memory: 100Mi
        limits:
          cpu: 500m
          memory: 500Mi
`
	}
	return config.DatasetJobSpecYaml
}

func IsCascadingDeletionEnabled() bool {
	if config == nil {
		return false
	}
	return config.EnableCascadingDeletion
}

func ParseConfigFromFileContent(content string) error {
	f, err := os.CreateTemp("", "dataset-config-*")
	if err != nil {
		return err
	}
	_, err = f.Write([]byte(content))
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(f.Name())
	}()
	return ParseConfigFromFile(f.Name())
}

func ParseConfigFromFile(configPath string) error {
	cfg := &configuration{}
	viper.SetConfigType("yaml")
	viper.SetConfigFile(configPath)
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	if err := viper.ReadInConfig(); err != nil {
		return err
	}
	err := viper.Unmarshal(cfg, func(c *mapstructure.DecoderConfig) {
		c.TagName = "json"
	})
	config = cfg
	if err != nil {
		return err
	}
	return nil
}
