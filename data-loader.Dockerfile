# Build the manager binary
ARG HADOOP_IMG=apache/hadoop:3.4.2
############################
# Hadoop client builder
############################
FROM --platform=$BUILDPLATFORM ${HADOOP_IMG} AS hadoop

FROM --platform=$BUILDPLATFORM mysql:8.4 AS mysqlcli

FROM --platform=$BUILDPLATFORM golang:1.25 AS builder
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
ARG JAVA_HOME=/opt/java
ARG HADOOP_HOME=/opt/hadoop
ENV JAVA_HOME=${JAVA_HOME} \
    HADOOP_HOME=${HADOOP_HOME} \
    PATH=${PATH}:${HADOOP_HOME}/bin:${JAVA_HOME}/bin

RUN DEBIAN_FRONTEND=noninteractive apt-get update -yq && \
    apt-get install -yq --no-install-recommends ca-certificates && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/* && \
    pip install --no-cache-dir "huggingface_hub[cli]"==0.33.1 modelscope==1.27.1 setuptools && \
    rclone_version=v1.70.1 && \
    arch=$(uname -m | sed -E 's/x86_64/amd64/g;s/aarch64/arm64/g') && \
    filename=rclone-${rclone_version}-linux-${arch} && \
    wget https://github.com/rclone/rclone/releases/download/${rclone_version}/${filename}.zip -O ${filename}.zip && \
    unzip ${filename}.zip && mv ${filename}/rclone /usr/local/bin && rm -rf ${filename} ${filename}.zip && \
    jre_arch=$(uname -m | sed -E 's/x86_64/x64/g;s/aarch64/aarch64/g') && \
    wget https://github.com/adoptium/temurin11-binaries/releases/download/jdk-11.0.30%2B7/OpenJDK11U-jre_${jre_arch}_linux_hotspot_11.0.30_7.tar.gz -O jre.tar.gz && \
    tar -zxf jre.tar.gz -C /opt && \
    mv /opt/jdk-11* /opt/java && \
    rm jre.tar.gz

COPY --from=builder /workspace/data-loader /usr/local/bin/

COPY --from=mysqlcli /usr/bin/mysql /usr/bin/mysql
############################
# Hadoop client (HDFS only)
############################
WORKDIR ${HADOOP_HOME}

COPY --from=hadoop ${HADOOP_HOME}/bin/hdfs ./bin/hdfs
COPY --from=hadoop ${HADOOP_HOME}/libexec ./libexec
COPY --from=hadoop ${HADOOP_HOME}/etc/hadoop ./etc/hadoop
COPY --from=hadoop ${HADOOP_HOME}/share/hadoop/common ./share/hadoop/common
COPY --from=hadoop ${HADOOP_HOME}/share/hadoop/hdfs ./share/hadoop/hdfs
RUN echo "export JAVA_HOME=${JAVA_HOME}" >> ${HADOOP_HOME}/etc/hadoop/hadoop-env.sh \
 && rm -rf bin/*.cmd libexec/*.cmd etc/hadoop/*.cmd \
 && mkdir -p share/hadoop/yarn share/hadoop/mapreduce \
 && find share/hadoop/common/lib/ \
      \( -name "jetty-*" -o -name "jersey-*" -o -name "netty-*" \) \
      -exec rm -rf {} +

ENTRYPOINT ["/usr/local/bin/data-loader"]
