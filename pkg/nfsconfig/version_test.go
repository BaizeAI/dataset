package nfsconfig

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestVersionContract(t *testing.T) {
	for _, version := range []string{"3", "4.0", "4.1", "4.2"} {
		t.Run(version, func(t *testing.T) {
			options := map[string]string{VersionKey: version}
			require.NoError(t, ValidateOptions("NFS", options))
			actual, err := Resolve(options, "4.2")
			require.NoError(t, err)
			require.Equal(t, version, actual)
			require.Error(t, ValidateOptions("REFERENCE", options))
		})
	}
	for _, version := range []string{"", "4", "5", "auto", " 4.1", "4.1 "} {
		require.Error(t, ValidateOptions("NFS", map[string]string{VersionKey: version}))
		_, err := Resolve(map[string]string{VersionKey: version}, DefaultVersion)
		require.Error(t, err)
	}
	for _, options := range []map[string]string{nil, {}, {"other": "value"}} {
		require.NoError(t, ValidateOptions("REFERENCE", options))
		actual, err := Resolve(options, "3")
		require.NoError(t, err)
		require.Equal(t, "3", actual)
	}
	_, err := Resolve(nil, "auto")
	require.Error(t, err)
}
