package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDatasetNFSVersion(t *testing.T) {
	tests := []struct {
		name       string
		configData string
		envValue   string
		want       string
	}{
		{
			name:       "default",
			configData: "enable_cascading_deletion: false",
			want:       "4.1",
		},
		{
			name:       "config value",
			configData: "dataset_nfs_version: \"4.0\"",
			want:       "4.0",
		},
		{
			name:       "nfs 3 config value",
			configData: "dataset_nfs_version: \"3\"",
			want:       "3",
		},
		{
			name:       "nfs 4.2 config value",
			configData: "dataset_nfs_version: \"4.2\"",
			want:       "4.2",
		},
		{
			name:       "environment overrides config",
			configData: "dataset_nfs_version: \"4.1\"",
			envValue:   "4.0",
			want:       "4.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(datasetNFSVersionEnv, tt.envValue)
			require.NoError(t, ParseConfigFromFileContent(tt.configData))
			assert.Equal(t, tt.want, GetDatasetNFSVersion())
		})
	}
}

func TestParseConfigRejectsUnsupportedDatasetNFSVersion(t *testing.T) {
	t.Setenv(datasetNFSVersionEnv, "5")
	err := ParseConfigFromFileContent("dataset_nfs_version: \"4.1\"")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported dataset NFS version")
}
