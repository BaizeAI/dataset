# Dataset: Simplified Data Management and Sharing for Kubernetes

[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

## Introduction

**Dataset** is a Kubernetes-native tool designed to simplify data management and sharing across AI/ML workflows. It leverages Persistent Volume Claims (PVCs) to preload datasets and models from public sources like Huggingface or S3 into local Kubernetes clusters. This eliminates the need for custom data loaders in individual workloads and ensures seamless data sharing across namespaces.

With Dataset, teams can efficiently manage and access data in multi-tenant environments while maintaining compatibility with any Kubernetes CSI driver. Its simplicity and ease of use make it an ideal choice for organizations looking to streamline AI/ML workflows without adding operational complexity.

## Key Features

- **Preloaded Datasets**: Load data from external sources into PVCs for immediate use in training and inference tasks.
- **Cross-Namespace Data Sharing**: Securely share data across namespaces, overcoming the traditional limitations of PVCs.
- **Kubernetes-Native Design**: Fully compatible with any Kubernetes CSI driver, avoiding reliance on external technologies like FUSE.
- **Cascading Deletion**: Optional feature to automatically delete dependent datasets when source datasets are removed, ensuring data consistency.
- **Operational Simplicity**: Designed for easy deployment and maintenance, with minimal overhead.

## Benefits

- **Streamlined Workflows**: Eliminates repetitive data-loading logic, allowing teams to focus on core AI/ML development.
- **Enhanced Collaboration**: Enables secure, efficient data sharing in multi-tenant Kubernetes environments.
- **Data Consistency**: Automatic cleanup of dependent resources prevents orphaned references and maintains data integrity.
- **Scalable and Reliable**: Works seamlessly with Kubernetes-native resources, ensuring compatibility and stability.

## Configuration

The Dataset controller supports configurable options through a YAML configuration file:

### Cascading Deletion

When enabled, cascading deletion automatically removes reference datasets when their source dataset is deleted:

```yaml
# Enable cascading deletion (default: false)
enable_cascading_deletion: true
```

**Important**: This feature should be used with caution as it will automatically delete datasets that reference the source dataset. Consider the impact on dependent workloads before enabling this feature.
