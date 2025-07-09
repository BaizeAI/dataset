package datasources

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// ConvertBandwidthLimitToKBps converts bandwidth limit from rclone format to KB/s for trickle
// rclone format: plain numbers are KiB/s, suffixes B|K|M|G|T|P supported
// trickle format: KB/s (kilobytes per second)
func ConvertBandwidthLimitToKBps(limit string) (int, error) {
	if limit == "" {
		return 0, nil
	}

	// Parse the number and suffix
	re := regexp.MustCompile(`^(\d+(?:\.\d+)?)([BKMGTP]?)$`)
	matches := re.FindStringSubmatch(strings.ToUpper(limit))
	if len(matches) != 3 {
		return 0, fmt.Errorf("invalid bandwidth limit format: %s", limit)
	}

	number, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number in bandwidth limit: %s", limit)
	}

	suffix := matches[2]
	
	// Convert to bytes per second first
	var bytesPerSecond float64
	switch suffix {
	case "B":
		bytesPerSecond = number
	case "", "K":
		bytesPerSecond = number * 1024 // KiB to bytes (plain number defaults to KiB/s for rclone)
	case "M":
		bytesPerSecond = number * 1024 * 1024 // MiB to bytes
	case "G":
		bytesPerSecond = number * 1024 * 1024 * 1024 // GiB to bytes
	case "T":
		bytesPerSecond = number * 1024 * 1024 * 1024 * 1024 // TiB to bytes
	case "P":
		bytesPerSecond = number * 1024 * 1024 * 1024 * 1024 * 1024 // PiB to bytes
	default:
		return 0, fmt.Errorf("unsupported suffix: %s", suffix)
	}

	// Convert bytes per second to KB/s (1 KB = 1000 bytes for trickle)
	kbps := int(bytesPerSecond / 1000)
	if kbps == 0 && bytesPerSecond > 0 {
		kbps = 1 // Minimum 1 KB/s
	}

	return kbps, nil
}

// WrapCommandWithBandwidthLimit wraps a command with trickle for bandwidth limiting
func WrapCommandWithBandwidthLimit(cmd *exec.Cmd, bandwidthLimit string) (*exec.Cmd, error) {
	if bandwidthLimit == "" {
		return cmd, nil
	}

	kbps, err := ConvertBandwidthLimitToKBps(bandwidthLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to convert bandwidth limit: %w", err)
	}

	if kbps <= 0 {
		return cmd, nil
	}

	// Create new command with trickle wrapper
	// trickle -d <download_rate> -u <upload_rate> <original_command>
	args := []string{
		"-d", strconv.Itoa(kbps), // download rate in KB/s
		"-u", strconv.Itoa(kbps), // upload rate in KB/s  
	}
	args = append(args, cmd.Path)
	args = append(args, cmd.Args[1:]...) // Skip the first arg which is the command name

	wrappedCmd := exec.Command("trickle", args...)
	wrappedCmd.Dir = cmd.Dir
	wrappedCmd.Env = cmd.Env
	wrappedCmd.Stdin = cmd.Stdin
	wrappedCmd.Stdout = cmd.Stdout
	wrappedCmd.Stderr = cmd.Stderr

	return wrappedCmd, nil
}