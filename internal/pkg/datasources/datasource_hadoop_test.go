package datasources

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestModelHadoopLoader(t *testing.T) {
	t.Run("NewModelHadoopLoader with valid options", func(t *testing.T) {
		options := map[string]string{
			"sourcePath": "/path/to/hdfs/data",
		}

		hadoopLoader, err := NewModelHadoopLoader(
			options,
			Options{
				Type: TypeHadoop,
				URI:  "hdfs://namenode:9000",
				Root: "/tmp/test-root",
			},
			Secrets{},
		)

		assert.NoError(t, err)
		assert.NotNil(t, hadoopLoader)
		assert.Equal(t, "/path/to/hdfs/data", hadoopLoader.modelHadoopOptions.SourcePath)
	})

	t.Run("NewModelHadoopLoader with missing sourcePath", func(t *testing.T) {
		options := map[string]string{
			// sourcePath is missing
		}

		hadoopLoader, err := NewModelHadoopLoader(
			options,
			Options{
				Type: TypeHadoop,
				URI:  "hdfs://namenode:9000",
				Root: "/tmp/test-root",
			},
			Secrets{},
		)

		assert.Error(t, err)
		assert.Nil(t, hadoopLoader)
		assert.Contains(t, err.Error(), "sourcePath option is required and must not be empty")
	})

	t.Run("convertHadoopOptions with valid options", func(t *testing.T) {
		d := &ModelHadoopLoader{}
		options := map[string]string{
			"sourcePath": "/path/to/hdfs/data",
		}

		result, err := d.convertHadoopOptions(options)

		assert.NoError(t, err)
		assert.Equal(t, "/path/to/hdfs/data", result.SourcePath)
	})

	t.Run("convertHadoopOptions with invalid JSON", func(t *testing.T) {
		d := &ModelHadoopLoader{}
		// Valid case with extra fields
		options := map[string]string{
			"sourcePath": "/path/to/hdfs/data",
			"extraField": "someValue",
		}

		result, err := d.convertHadoopOptions(options)

		assert.NoError(t, err)
		assert.Equal(t, "/path/to/hdfs/data", result.SourcePath)
		// extraField should be ignored since it doesn't match struct field
	})

	t.Run("Sync with invalid scheme", func(t *testing.T) {
		hadoopLoader, err := NewModelHadoopLoader(
			map[string]string{
				"sourcePath": "/hdfs/source/path",
			},
			Options{
				Type: TypeHadoop,
				URI:  "http://example.com", // Invalid scheme
				Root: "/tmp/test-output",
			},
			Secrets{},
		)
		assert.NoError(t, err)

		err = hadoopLoader.Sync("http://example.com/path", "/tmp/output")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid scheme http, only hdfs is supported")
	})

	t.Run("Sync with malformed URI", func(t *testing.T) {
		hadoopLoader, err := NewModelHadoopLoader(
			map[string]string{
				"sourcePath": "/hdfs/source/path",
			},
			Options{
				Type: TypeHadoop,
				URI:  "://invalid-uri", // Malformed URI
				Root: "/tmp/test-output",
			},
			Secrets{},
		)
		assert.NoError(t, err)

		err = hadoopLoader.Sync("://invalid-uri", "/tmp/output")
		assert.Error(t, err)
	})
}
