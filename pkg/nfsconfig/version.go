// Package nfsconfig defines the NFS version contract shared by Dataset and Baize.
package nfsconfig

import "fmt"

const (
	DefaultVersion = "4.1"
	VersionKey     = "nfsVersion"
	EnvironmentKey = "DATASET_NFS_VERSION"
	ConfigKey      = "dataset_nfs_version"
)

// Validate accepts an explicit protocol version. Callers handle absent values.
func Validate(version string) error {
	switch version {
	case "3", "4.0", "4.1", "4.2":
		return nil
	default:
		return fmt.Errorf("unsupported dataset NFS version %q, must be one of: 3, 4.0, 4.1, 4.2", version)
	}
}

// ValidateOptions distinguishes an absent version key from an explicitly empty one.
func ValidateOptions(sourceType string, options map[string]string) error {
	version, present := options[VersionKey]
	if !present {
		return nil
	}
	if sourceType != "NFS" {
		return fmt.Errorf("%s is only supported for NFS datasets", VersionKey)
	}
	return Validate(version)
}

// Resolve applies the controller default only when the version key is absent.
func Resolve(options map[string]string, fallback string) (string, error) {
	version, present := options[VersionKey]
	if !present {
		version = fallback
	}
	if err := Validate(version); err != nil {
		return "", err
	}
	return version, nil
}
