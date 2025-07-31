# Example: Cascading Deletion of Reference Datasets

This document demonstrates how cascading deletion works with the Dataset controller.

## Scenario

1. **Source Dataset**: A dataset with type `GIT` that contains shared data
2. **Reference Datasets**: Multiple datasets with type `REFERENCE` that point to the source dataset
3. **Cascading Deletion**: When the source dataset is deleted, all reference datasets are automatically deleted

## Configuration

To enable cascading deletion, set the following in your controller configuration:

```yaml
enable_cascading_deletion: true
```

## Example Datasets

### Source Dataset
```yaml
apiVersion: dataset.baizeai.io/v1alpha1
kind: Dataset
metadata:
  name: shared-model
  namespace: ml-models
spec:
  share: true  # Enable sharing
  source:
    type: GIT
    uri: https://github.com/huggingface/transformers.git
```

### Reference Dataset 1
```yaml
apiVersion: dataset.baizeai.io/v1alpha1
kind: Dataset
metadata:
  name: training-model
  namespace: ml-training
spec:
  source:
    type: REFERENCE
    uri: dataset://ml-models/shared-model  # References the source dataset
```

### Reference Dataset 2
```yaml
apiVersion: dataset.baizeai.io/v1alpha1
kind: Dataset
metadata:
  name: inference-model  
  namespace: ml-inference
spec:
  source:
    type: REFERENCE
    uri: dataset://ml-models/shared-model  # References the source dataset
```

## Deletion Behavior

### Without Cascading Deletion (default)
When `shared-model` is deleted:
- Only `shared-model` is deleted
- `training-model` and `inference-model` remain but become invalid (broken references)
- Manual cleanup required

### With Cascading Deletion (enabled)
When `shared-model` is deleted:
- `shared-model` is deleted
- `training-model` is automatically deleted
- `inference-model` is automatically deleted  
- Associated PVs with retain policy are also cleaned up
- No manual cleanup required

## Safety Considerations

- **Default Disabled**: Cascading deletion is disabled by default for safety
- **Impact Assessment**: Consider all dependent workloads before enabling
- **Testing**: Test in non-production environments first
- **Monitoring**: Monitor deletion events and ensure expected behavior

## Enabling Cascading Deletion

1. Update your controller configuration:
   ```yaml
   enable_cascading_deletion: true
   ```

2. Restart the controller to apply the configuration

3. Verify the setting is applied by checking the controller logs

## Troubleshooting

- Check controller logs for cascading deletion messages
- Verify configuration is loaded correctly
- Ensure RBAC permissions are sufficient for cross-namespace operations
- Test with non-critical datasets first