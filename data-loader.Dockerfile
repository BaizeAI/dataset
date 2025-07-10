# Build the manager binary
FROM --platform=$BUILDPLATFORM golang:1.24 as builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -ldflags "-s -w" -a -o data-loader ./cmd/data-loader

FROM python:3.13

RUN pip install --no-cache-dir "huggingface_hub[cli]"==0.33.1 modelscope==1.27.1 setuptools && \
    rclone_version=v1.70.1 && \
    arch=$(uname -m | sed -E 's/x86_64/amd64/g;s/aarch64/arm64/g') && \
    filename=rclone-${rclone_version}-linux-${arch} && \
    wget https://github.com/rclone/rclone/releases/download/${rclone_version}/${filename}.zip -O ${filename}.zip && \
    unzip ${filename}.zip && mv ${filename}/rclone /usr/local/bin && rm -rf ${filename} ${filename}.zip && \
    apt-get update && apt-get install -y trickle && rm -rf /var/lib/apt/lists/*


COPY --from=builder /workspace/data-loader /usr/local/bin/
COPY entrypoint.sh /usr/local/bin/

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
