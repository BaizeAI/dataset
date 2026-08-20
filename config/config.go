package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

var (
	config *configuration
)

const (
	defaultDatasetNFSVersion = "4.1"
	datasetNFSVersionEnv     = "DATASET_NFS_VERSION"
)

type configuration struct {
	DatasetJobSpecYaml      string `json:"dataset_job_spec_yaml"`
	EnableCascadingDeletion bool   `json:"enable_cascading_deletion"`
	DatasetNFSVersion       string `json:"dataset_nfs_version"`
}

func validateDatasetNFSVersion(version string) error {
	switch version {
	case "3", "4.0", "4.1", "4.2":
		return nil
	default:
		return fmt.Errorf("unsupported dataset NFS version %q, must be one of: 3, 4.0, 4.1, 4.2", version)
	}
}

// GetDatasetNFSVersion returns the NFS protocol version used for newly-created NFS PVs.
// DATASET_NFS_VERSION is bound during configuration loading and takes precedence over
// the config file value. The default keeps the existing behavior at NFS 4.1.
func GetDatasetNFSVersion() string {
	if config == nil || config.DatasetNFSVersion == "" {
		return defaultDatasetNFSVersion
	}
	return config.DatasetNFSVersion
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
	viper.SetDefault("dataset_nfs_version", defaultDatasetNFSVersion)
	if err := viper.BindEnv("dataset_nfs_version", datasetNFSVersionEnv); err != nil {
		return err
	}
	if err := viper.ReadInConfig(); err != nil {
		return err
	}
	err := viper.Unmarshal(cfg, func(c *mapstructure.DecoderConfig) {
		c.TagName = "json"
	})
	if err != nil {
		return err
	}
	cfg.DatasetNFSVersion = strings.TrimSpace(viper.GetString("dataset_nfs_version"))
	if cfg.DatasetNFSVersion == "" {
		cfg.DatasetNFSVersion = defaultDatasetNFSVersion
	}
	if err := validateDatasetNFSVersion(cfg.DatasetNFSVersion); err != nil {
		return err
	}
	config = cfg
	return nil
}
