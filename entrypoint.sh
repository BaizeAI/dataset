#!/bin/bash
set -e

# Function to convert bandwidth limit from rclone format to KB/s for trickle
convert_bandwidth_limit() {
    local limit="$1"
    
    if [[ -z "$limit" ]]; then
        echo "0"
        return
    fi
    
    # Extract number and suffix using regex
    if [[ "$limit" =~ ^([0-9]+(\.[0-9]+)?)([BKMGTP]?)$ ]]; then
        local number="${BASH_REMATCH[1]}"
        local suffix="${BASH_REMATCH[3]^^}"  # Convert to uppercase
        
        # Convert to integer for bash arithmetic (handle decimal by multiplying by 1000 first)
        local number_int
        if [[ "$number" =~ \. ]]; then
            # For decimal numbers, multiply by 1000 first then divide later
            number_int=$(awk "BEGIN {print int($number * 1000)}")
            local decimal_multiplier=1000
        else
            number_int=$number
            local decimal_multiplier=1
        fi
        
        local bytes_per_second
        case "$suffix" in
            "B")
                bytes_per_second=$((number_int / decimal_multiplier))
                ;;
            ""|"K")
                # Plain number defaults to KiB/s for rclone compatibility
                bytes_per_second=$((number_int * 1024 / decimal_multiplier))
                ;;
            "M")
                bytes_per_second=$((number_int * 1024 * 1024 / decimal_multiplier))
                ;;
            "G")
                bytes_per_second=$((number_int * 1024 * 1024 * 1024 / decimal_multiplier))
                ;;
            "T")
                bytes_per_second=$((number_int * 1024 * 1024 * 1024 * 1024 / decimal_multiplier))
                ;;
            "P")
                bytes_per_second=$((number_int * 1024 * 1024 * 1024 * 1024 * 1024 / decimal_multiplier))
                ;;
            *)
                echo "Error: Unsupported suffix: $suffix" >&2
                echo "0"
                return
                ;;
        esac
        
        # Convert bytes per second to KB/s (1 KB = 1000 bytes for trickle)
        local kbps=$((bytes_per_second / 1000))
        
        # Minimum 1 KB/s if > 0
        if [[ $bytes_per_second -gt 0 && $kbps -eq 0 ]]; then
            kbps=1
        fi
        
        echo "$kbps"
    else
        echo "Error: Invalid bandwidth limit format: $limit" >&2
        echo "0"
    fi
}

# Check if bandwidth limit is set via environment variable
if [[ -n "$BANDWIDTH_LIMIT" ]]; then
    echo "Bandwidth limit detected: $BANDWIDTH_LIMIT"
    
    # Convert bandwidth limit to trickle format
    kbps=$(convert_bandwidth_limit "$BANDWIDTH_LIMIT")
    
    if [[ "$kbps" -gt 0 ]]; then
        echo "Applying bandwidth limit: ${kbps} KB/s"
        # Use trickle to wrap the data-loader command
        exec trickle -d "$kbps" -u "$kbps" /usr/local/bin/data-loader "$@"
    else
        echo "Invalid or zero bandwidth limit, running without trickle"
        exec /usr/local/bin/data-loader "$@"
    fi
else
    # No bandwidth limit, run data-loader directly
    exec /usr/local/bin/data-loader "$@"
fi