package datasources

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertBandwidthLimitToKBps(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
		hasError bool
	}{
		{
			name:     "empty string",
			input:    "",
			expected: 0,
			hasError: false,
		},
		{
			name:     "plain number (KiB/s)",
			input:    "100",
			expected: 102, // 100 * 1024 / 1000
			hasError: false,
		},
		{
			name:     "bytes per second",
			input:    "1000B",
			expected: 1,
			hasError: false,
		},
		{
			name:     "kilobytes",
			input:    "10K",
			expected: 10, // 10 * 1024 / 1000
			hasError: false,
		},
		{
			name:     "megabytes",
			input:    "1M",
			expected: 1048, // 1 * 1024 * 1024 / 1000
			hasError: false,
		},
		{
			name:     "gigabytes",
			input:    "1G",
			expected: 1073741, // 1 * 1024^3 / 1000
			hasError: false,
		},
		{
			name:     "decimal number",
			input:    "1.5M",
			expected: 1572, // 1.5 * 1024 * 1024 / 1000
			hasError: false,
		},
		{
			name:     "lowercase suffix",
			input:    "10m",
			expected: 10485, // 10 * 1024 * 1024 / 1000
			hasError: false,
		},
		{
			name:     "invalid format",
			input:    "invalid",
			expected: 0,
			hasError: true,
		},
		{
			name:     "negative number",
			input:    "-10M",
			expected: 0,
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ConvertBandwidthLimitToKBps(tt.input)
			if tt.hasError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestWrapCommandWithBandwidthLimit(t *testing.T) {
	t.Run("no bandwidth limit", func(t *testing.T) {
		originalCmd := exec.Command("git", "clone", "https://example.com/repo.git")
		wrappedCmd, err := WrapCommandWithBandwidthLimit(originalCmd, "")
		
		require.NoError(t, err)
		assert.Equal(t, originalCmd, wrappedCmd)
	})

	t.Run("with bandwidth limit", func(t *testing.T) {
		originalCmd := exec.Command("git", "clone", "https://example.com/repo.git")
		originalCmd.Dir = "/tmp"
		originalCmd.Env = []string{"TEST=1"}
		
		wrappedCmd, err := WrapCommandWithBandwidthLimit(originalCmd, "10M")
		
		require.NoError(t, err)
		assert.Equal(t, "trickle", wrappedCmd.Path)
		assert.Equal(t, "trickle", wrappedCmd.Args[0])
		assert.Equal(t, "-d", wrappedCmd.Args[1])
		assert.Equal(t, "10485", wrappedCmd.Args[2])
		assert.Equal(t, "-u", wrappedCmd.Args[3])
		assert.Equal(t, "10485", wrappedCmd.Args[4])
		assert.Contains(t, wrappedCmd.Args[5], "git") // Path might be resolved to full path
		assert.Equal(t, "clone", wrappedCmd.Args[6])
		assert.Equal(t, "https://example.com/repo.git", wrappedCmd.Args[7])
		assert.Equal(t, "/tmp", wrappedCmd.Dir)
		assert.Equal(t, []string{"TEST=1"}, wrappedCmd.Env)
	})

	t.Run("invalid bandwidth limit", func(t *testing.T) {
		originalCmd := exec.Command("git", "clone", "https://example.com/repo.git")
		_, err := WrapCommandWithBandwidthLimit(originalCmd, "invalid")
		
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to convert bandwidth limit")
	})

	t.Run("zero bandwidth limit", func(t *testing.T) {
		originalCmd := exec.Command("git", "clone", "https://example.com/repo.git")
		wrappedCmd, err := WrapCommandWithBandwidthLimit(originalCmd, "0")
		
		require.NoError(t, err)
		assert.Equal(t, originalCmd, wrappedCmd)
	})
}