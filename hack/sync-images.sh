#!/bin/bash
# Usage: this script is used to create issues as the following template:
# https://github.com/DaoCloud/public-image-mirror/issues/new?assignees=&labels=&projects=&template=sync-image.md&title=SYNC+IMAGE
# To trigger image sync from ghcr.io to ghcr.m.daocloud.io
set -o errexit
set -o nounset
set -o pipefail

source "hack/util.sh"

mirror_repo_name="DaoCloud/public-image-mirror"
REPO_ROOT=$(cd "$(dirname -- "${BASH_SOURCE[0]}")/.."; printf %s "$PWD")

if util::cmd_exist gh; then
    echo "gh already installed"
else
    echo "install gh..."
    bash $REPO_ROOT/hack/install-gh.sh
fi

if gh auth status; then
    echo "passed"
else
    echo "not pass"
    if [[ -z "${GITHUB_TOKEN}" ]]; then
        echo "GITHUB_TOKEN is not set"
        exit 1
    fi
    gh auth login --with-token <<< "${GITHUB_TOKEN}"
fi

helm repo add dataset https://baizeai.github.io/charts && helm repo update dataset

images=$(helm template dataset/dataset | grep "image:" | awk '{print $2}' |  tr -d '"')
images=($(printf "%s\n" "${images[@]}" | sort -u))
arch_list=(linux/amd64 linux/arm64)

function create_issue() {
    image=$1
    arch=$2
    echo "sync ${image}"
    gh -R $mirror_repo_name issue create \
    --title "${image}" \
    --body "### 镜像架构\n\n${arch}" \
    --label "sync image"
}

# Creates issues to trigger image sync
for image in "${images[@]}"; do
    for arch in "${arch_list[@]}"; do
        create_issue "${image}" "${arch}"
        sleep 3
    done
done

# Checks if all issues are closed
issues="SYNC IMAGE"
while [[ "${issues}" != "" ]]; do
    issues=`gh -R $mirror_repo_name issue list --author "@me" --state open | { grep "SYNC IMAGE" || true; } `
    echo "waiting for issues to be closed: "
    echo "${issues}"
    sleep 10
done

echo "All issues are closed, images were synced"
