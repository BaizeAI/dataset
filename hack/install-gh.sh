#!/bin/bash

set -euo pipefail

VERSION="2.37.0"

if [ -n "$(which gh)" ]; then
    echo "gh is already installed"
    exit 0
fi

# if os is linux
if [ "$(uname)" == "Linux" ]; then
    curl -sSL https://github.com/cli/cli/releases/download/v${VERSION}/gh_${VERSION}_linux_amd64.tar.gz -o gh_${VERSION}_linux_amd64.tar.gz
    tar xvf gh_${VERSION}_linux_amd64.tar.gz
    cp gh_${VERSION}_linux_amd64/bin/gh /usr/local/bin/
    exit 0
elif [ "$(uname)" == "Darwin" ]; then
    brew install gh
    exit 0
else
    echo "Unknown OS, please install gh manually:" >&2
    echo "\thttps://github.com/cli/cli#installation" >&2
    exit 1
fi