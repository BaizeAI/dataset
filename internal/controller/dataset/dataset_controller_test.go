/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package dataset

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	datasetv1alpha1 "github.com/BaizeAI/dataset/api/dataset/v1alpha1"
	"github.com/BaizeAI/dataset/config"
	"github.com/BaizeAI/dataset/internal/pkg/constants"
	"github.com/BaizeAI/dataset/pkg/kubeutils"
	"github.com/BaizeAI/dataset/pkg/mountpolicy"
)

func TestDatasetReconciler_findReferencingDatasets(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, datasetv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	// Create a source dataset
	sourceDs := &datasetv1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "source-dataset",
			Namespace: "default",
		},
		Spec: datasetv1alpha1.DatasetSpec{
			Share: true,
			Source: datasetv1alpha1.DatasetSource{
				Type: datasetv1alpha1.DatasetTypeGit,
				URI:  "https://github.com/example/repo.git",
			},
		},
	}

	// Create a referencing dataset
	refDs1 := &datasetv1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ref-dataset-1",
			Namespace: "namespace1",
		},
		Spec: datasetv1alpha1.DatasetSpec{
			Source: datasetv1alpha1.DatasetSource{
				Type: datasetv1alpha1.DatasetTypeReference,
				URI:  "dataset://default/source-dataset",
			},
		},
	}

	// Create another referencing dataset
	refDs2 := &datasetv1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ref-dataset-2",
			Namespace: "namespace2",
		},
		Spec: datasetv1alpha1.DatasetSpec{
			Source: datasetv1alpha1.DatasetSource{
				Type: datasetv1alpha1.DatasetTypeReference,
				URI:  "dataset://default/source-dataset",
			},
		},
	}

	// Create a non-referencing dataset
	nonRefDs := &datasetv1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "non-ref-dataset",
			Namespace: "namespace3",
		},
		Spec: datasetv1alpha1.DatasetSpec{
			Source: datasetv1alpha1.DatasetSource{
				Type: datasetv1alpha1.DatasetTypeGit,
				URI:  "https://github.com/example/other-repo.git",
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(sourceDs, refDs1, refDs2, nonRefDs).
		Build()

	reconciler := &DatasetReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	ctx := context.Background()
	referencingDatasets, err := reconciler.findReferencingDatasets(ctx, sourceDs)

	require.NoError(t, err)
	assert.Len(t, referencingDatasets, 2)

	// Check that we found the correct referencing datasets
	foundNames := make(map[string]bool)
	for _, ds := range referencingDatasets {
		foundNames[ds.Name] = true
	}

	assert.True(t, foundNames["ref-dataset-1"])
	assert.True(t, foundNames["ref-dataset-2"])
	assert.False(t, foundNames["non-ref-dataset"])
	assert.False(t, foundNames["source-dataset"])
}

func TestDatasetReconciler_enqueueReferenceDatasetsScopesDependencies(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, datasetv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	source := &datasetv1alpha1.Dataset{ObjectMeta: metav1.ObjectMeta{Name: "source", Namespace: "origin"}}
	middle := &datasetv1alpha1.Dataset{ObjectMeta: metav1.ObjectMeta{Name: "middle", Namespace: "middle"}, Spec: datasetv1alpha1.DatasetSpec{Source: datasetv1alpha1.DatasetSource{Type: datasetv1alpha1.DatasetTypeReference, URI: "dataset://origin/source"}}}
	leaf := &datasetv1alpha1.Dataset{ObjectMeta: metav1.ObjectMeta{Name: "leaf", Namespace: "target"}, Spec: datasetv1alpha1.DatasetSpec{Source: datasetv1alpha1.DatasetSource{Type: datasetv1alpha1.DatasetTypeReference, URI: "dataset://middle/middle"}}}
	unrelated := &datasetv1alpha1.Dataset{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "other"}, Spec: datasetv1alpha1.DatasetSpec{Source: datasetv1alpha1.DatasetSource{Type: datasetv1alpha1.DatasetTypeReference, URI: "dataset://other/root"}}}
	reconciler := &DatasetReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(source, middle, leaf, unrelated).Build(), Scheme: scheme}

	requests := reconciler.enqueueReferenceDatasets(context.Background(), source)
	require.ElementsMatch(t, []client.ObjectKey{{Namespace: "middle", Name: "middle"}, {Namespace: "target", Name: "leaf"}}, requestKeys(requests))

	requests = reconciler.enqueueReferenceDatasets(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "target"}})
	require.Equal(t, []client.ObjectKey{{Namespace: "target", Name: "leaf"}}, requestKeys(requests))

	oldDataset := source.DeepCopy()
	newDataset := source.DeepCopy()
	require.False(t, dependencyDatasetChanged(event.UpdateEvent{ObjectOld: oldDataset, ObjectNew: newDataset}))
	newDataset.Status.PVCName = "source-pvc"
	require.True(t, dependencyDatasetChanged(event.UpdateEvent{ObjectOld: oldDataset, ObjectNew: newDataset}))
}

func requestKeys(requests []reconcile.Request) []client.ObjectKey {
	keys := make([]client.ObjectKey, 0, len(requests))
	for _, request := range requests {
		keys = append(keys, request.NamespacedName)
	}
	return keys
}
func TestDatasetReconciler_reconcileCascadingDeletion_Disabled(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, datasetv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	// Test configuration with cascading deletion disabled
	err := config.ParseConfigFromFileContent("enable_cascading_deletion: false")
	require.NoError(t, err)

	sourceDs := &datasetv1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "source-dataset",
			Namespace:         "default",
			DeletionTimestamp: &metav1.Time{Time: time.Now()},
			Finalizers:        []string{"dataset-controller"},
		},
		Spec: datasetv1alpha1.DatasetSpec{
			Share: true,
			Source: datasetv1alpha1.DatasetSource{
				Type: datasetv1alpha1.DatasetTypeGit,
				URI:  "https://github.com/example/repo.git",
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(sourceDs).
		Build()

	reconciler := &DatasetReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	ctx := context.Background()
	err = reconciler.reconcileCascadingDeletion(ctx, sourceDs)

	// Should not error and should do nothing when cascading deletion is disabled
	require.NoError(t, err)
}

func TestDatasetReconciler_reconcileCascadingDeletion_Enabled(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, datasetv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	// Test configuration with cascading deletion enabled
	err := config.ParseConfigFromFileContent("enable_cascading_deletion: true")
	require.NoError(t, err)

	sourceDs := &datasetv1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "source-dataset",
			Namespace:         "default",
			DeletionTimestamp: &metav1.Time{Time: time.Now()},
			Finalizers:        []string{"dataset-controller"},
		},
		Spec: datasetv1alpha1.DatasetSpec{
			Share: true,
			Source: datasetv1alpha1.DatasetSource{
				Type: datasetv1alpha1.DatasetTypeGit,
				URI:  "https://github.com/example/repo.git",
			},
		},
	}

	refDs := &datasetv1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ref-dataset",
			Namespace: "namespace1",
		},
		Spec: datasetv1alpha1.DatasetSpec{
			Source: datasetv1alpha1.DatasetSource{
				Type: datasetv1alpha1.DatasetTypeReference,
				URI:  "dataset://default/source-dataset",
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(sourceDs, refDs).
		Build()

	reconciler := &DatasetReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	ctx := context.Background()
	err = reconciler.reconcileCascadingDeletion(ctx, sourceDs)

	require.NoError(t, err)

	// Check that the referencing dataset has been deleted
	updatedRefDs := &datasetv1alpha1.Dataset{}
	err = fakeClient.Get(ctx, types.NamespacedName{Name: "ref-dataset", Namespace: "namespace1"}, updatedRefDs)
	// The dataset should either be deleted (not found) or marked for deletion
	if err != nil {
		// Dataset was deleted completely
		require.True(t, client.IgnoreNotFound(err) == nil, "Expected dataset to be deleted or not found")
	} else {
		// Dataset exists but should be marked for deletion
		assert.NotNil(t, updatedRefDs.DeletionTimestamp, "Referencing dataset should be marked for deletion")
	}
}

func TestDatasetReconciler_cleanupRetainedPV(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, datasetv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	dsUID := types.UID("12345678-1234-1234-1234-123456789abc")
	ds := &datasetv1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-dataset",
			Namespace: "default",
			UID:       dsUID,
		},
		Spec: datasetv1alpha1.DatasetSpec{
			Source: datasetv1alpha1.DatasetSource{
				Type: datasetv1alpha1.DatasetTypeReference,
				URI:  "dataset://other/source-dataset",
			},
		},
	}

	// Create a retained PV that should be cleaned up
	pvName := "dataset-default-test-dataset-123456789abc"
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: pvName,
			Labels: map[string]string{
				constants.DatasetNameLabel: "test-dataset",
			},
		},
		Spec: corev1.PersistentVolumeSpec{
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ds, pv).
		Build()

	reconciler := &DatasetReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	ctx := context.Background()
	err := reconciler.cleanupRetainedPV(ctx, ds)

	require.NoError(t, err)

	// Check that the PV has been deleted
	deletedPV := &corev1.PersistentVolume{}
	err = fakeClient.Get(ctx, types.NamespacedName{Name: pvName}, deletedPV)
	assert.True(t, client.IgnoreNotFound(err) == nil, "PV should be deleted")
}

func TestDatasetReconciler_reconcileClaimPVC(t *testing.T) {
	tests := []struct {
		name        string
		pvcName     string
		pvc         *corev1.PersistentVolumeClaim
		dsName      string
		wantPVCName string
		wantErr     bool
		errContains string
	}{
		{
			name:    "successful reconciliation when PVC exists and is bound",
			pvcName: "existing-pvc",
			pvc: &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "existing-pvc",
					Namespace: "default",
					Labels:    map[string]string{},
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("10Gi"),
						},
					},
				},
				Status: corev1.PersistentVolumeClaimStatus{
					Phase: corev1.ClaimBound,
				},
			},
			dsName:      "test-dataset",
			wantPVCName: "existing-pvc",
			wantErr:     false,
		},
		{
			name:        "returns error when PVC does not exist",
			pvcName:     "non-existent-pvc",
			pvc:         nil,
			dsName:      "test-dataset",
			wantPVCName: "",
			wantErr:     true,
			errContains: "get pvc",
		},
		{
			name:    "returns error when PVC is not bound",
			pvcName: "pending-pvc",
			pvc: &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pending-pvc",
					Namespace: "default",
					Labels:    map[string]string{},
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("10Gi"),
						},
					},
				},
				Status: corev1.PersistentVolumeClaimStatus{
					Phase: corev1.ClaimPending,
				},
			},
			dsName:      "test-dataset",
			wantPVCName: "",
			wantErr:     true,
			errContains: "is not bound yet",
		},
		{
			name:    "successful reclamation when PVC is reused by same dataset",
			pvcName: "dataset-pvc",
			pvc: &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dataset-pvc",
					Namespace: "default",
					Labels: map[string]string{
						constants.DatasetNameLabel: "test-dataset",
					},
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("10Gi"),
						},
					},
				},
				Status: corev1.PersistentVolumeClaimStatus{
					Phase: corev1.ClaimBound,
				},
			},
			dsName:      "test-dataset",
			wantPVCName: "dataset-pvc",
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, datasetv1alpha1.AddToScheme(scheme))
			require.NoError(t, corev1.AddToScheme(scheme))

			var objects []client.Object
			if tt.pvc != nil {
				objects = append(objects, tt.pvc)
			}

			ds := &datasetv1alpha1.Dataset{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tt.dsName,
					Namespace: "default",
				},
				Spec: datasetv1alpha1.DatasetSpec{
					Source: datasetv1alpha1.DatasetSource{
						Type: datasetv1alpha1.DatasetTypeGit,
						URI:  "https://github.com/example/repo.git",
					},
					VolumeClaimRef: &datasetv1alpha1.VolumeClaimRef{
						Name: tt.pvcName,
					},
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()

			reconciler := &DatasetReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			ctx := context.Background()

			err := reconciler.reconcileClaimPVC(ctx, ds)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantPVCName, ds.Status.PVCName)
			}
		})
	}
}

func TestDatasetReconciler_reconcilePVCNFSVersion(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		want     string
	}{
		{name: "nfs 4.0", envValue: "4.0", want: "nfsvers=4.0"},
		{name: "nfs 4.1", envValue: "4.1", want: "nfsvers=4.1"},
		{name: "nfs 3", envValue: "3", want: "nfsvers=3"},
		{name: "nfs 4.2", envValue: "4.2", want: "nfsvers=4.2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DATASET_NFS_VERSION", tt.envValue)
			require.NoError(t, config.ParseConfigFromFileContent(""))

			scheme := runtime.NewScheme()
			require.NoError(t, datasetv1alpha1.AddToScheme(scheme))
			require.NoError(t, corev1.AddToScheme(scheme))

			ds := &datasetv1alpha1.Dataset{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "netapp-dataset",
					Namespace: "default",
				},
				Spec: datasetv1alpha1.DatasetSpec{
					Source: datasetv1alpha1.DatasetSource{
						Type: datasetv1alpha1.DatasetTypeNFS,
						URI:  "nfs://10.0.0.1/export/path",
					},
				},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			reconciler := &DatasetReconciler{Client: fakeClient, Scheme: scheme}

			require.NoError(t, reconciler.reconcilePVC(context.Background(), ds))

			pv := &corev1.PersistentVolume{}
			require.NoError(t, fakeClient.Get(context.Background(), client.ObjectKey{
				Name: "dataset-default-pvc-netapp-dataset",
			}, pv))
			assert.Equal(t, []string{tt.want}, pv.Spec.MountOptions)
		})
	}
}

func TestDatasetReconciler_reconcilePVCNFSVersionDoesNotUpdateExistingPV(t *testing.T) {
	t.Setenv("DATASET_NFS_VERSION", "4.0")
	require.NoError(t, config.ParseConfigFromFileContent(""))

	scheme := runtime.NewScheme()
	require.NoError(t, datasetv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	ds := &datasetv1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "netapp-dataset",
			Namespace: "default",
		},
		Spec: datasetv1alpha1.DatasetSpec{
			Source: datasetv1alpha1.DatasetSource{
				Type: datasetv1alpha1.DatasetTypeNFS,
				URI:  "nfs://10.0.0.1/export/path",
			},
		},
	}
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: "dataset-default-pvc-netapp-dataset",
			Labels: map[string]string{
				constants.DatasetNameLabel: ds.Name,
			},
		},
		Spec: corev1.PersistentVolumeSpec{
			MountOptions: []string{"nfsvers=4.1"},
		},
	}
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ds.Name,
			Namespace: ds.Namespace,
			Labels: map[string]string{
				constants.DatasetNameLabel: ds.Name,
			},
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pv, pvc).Build()
	reconciler := &DatasetReconciler{Client: fakeClient, Scheme: scheme}

	require.NoError(t, reconciler.reconcilePVC(context.Background(), ds))

	storedPV := &corev1.PersistentVolume{}
	require.NoError(t, fakeClient.Get(context.Background(), client.ObjectKey{Name: pv.Name}, storedPV))
	assert.Equal(t, []string{"nfsvers=4.1"}, storedPV.Spec.MountOptions)
}

func TestDatasetReconciler_reconcilePVCManual(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, datasetv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	storageClassName := "manual-storage"
	ds := &datasetv1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "manual-dataset",
			Namespace: "default",
			UID:       types.UID("manual-dataset-uid"),
		},
		Spec: datasetv1alpha1.DatasetSpec{
			Source: datasetv1alpha1.DatasetSource{
				Type: datasetv1alpha1.DatasetTypeManual,
				URI:  "manual://",
			},
			VolumeClaimTemplate: corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					StorageClassName: &storageClassName,
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("1Gi"),
						},
					},
				},
			},
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	reconciler := &DatasetReconciler{Client: fakeClient, Scheme: scheme}

	require.NoError(t, reconciler.reconcilePVC(context.Background(), ds))
	assert.Equal(t, ds.Name, ds.Status.PVCName)

	pvc := &corev1.PersistentVolumeClaim{}
	require.NoError(t, fakeClient.Get(context.Background(), client.ObjectKey{
		Namespace: ds.Namespace,
		Name:      ds.Name,
	}, pvc))
	assert.Equal(t, []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, pvc.Spec.AccessModes)
	require.NotNil(t, pvc.Spec.StorageClassName)
	assert.Equal(t, storageClassName, *pvc.Spec.StorageClassName)
	assert.True(t, pvc.Spec.Resources.Requests[corev1.ResourceStorage].Equal(resource.MustParse("1Gi")))
	assert.Equal(t, ds.Name, pvc.Labels[constants.DatasetNameLabel])
	require.Len(t, pvc.OwnerReferences, 1)
	assert.Equal(t, ds.UID, pvc.OwnerReferences[0].UID)
}

func TestDatasetReconciler_updateStatusDoesNotRequireLatestMetadata(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, datasetv1alpha1.AddToScheme(scheme))

	ctx := context.Background()
	ds := &datasetv1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "manual-dataset",
			Namespace: "public",
		},
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&datasetv1alpha1.Dataset{}).
		WithObjects(ds).
		Build()

	stale := &datasetv1alpha1.Dataset{}
	require.NoError(t, fakeClient.Get(ctx, client.ObjectKeyFromObject(ds), stale))

	concurrent := &datasetv1alpha1.Dataset{}
	require.NoError(t, fakeClient.Get(ctx, client.ObjectKeyFromObject(ds), concurrent))
	concurrent.Labels = map[string]string{"updated": "concurrently"}
	require.NoError(t, fakeClient.Update(ctx, concurrent))

	previousStatus := stale.Status.DeepCopy()
	stale.Status.Phase = datasetv1alpha1.DatasetStatusPhaseReady
	reconciler := &DatasetReconciler{Client: fakeClient, Scheme: scheme}
	require.NoError(t, reconciler.updateStatus(ctx, stale, previousStatus))

	updated := &datasetv1alpha1.Dataset{}
	require.NoError(t, fakeClient.Get(ctx, client.ObjectKeyFromObject(ds), updated))
	assert.Equal(t, datasetv1alpha1.DatasetStatusPhaseReady, updated.Status.Phase)
	assert.Equal(t, map[string]string{"updated": "concurrently"}, updated.Labels)
}

func TestManualDatasetDoesNotSupportPreload(t *testing.T) {
	ds := &datasetv1alpha1.Dataset{
		Spec: datasetv1alpha1.DatasetSpec{
			Source: datasetv1alpha1.DatasetSource{
				Type: datasetv1alpha1.DatasetTypeManual,
				URI:  "manual://",
			},
			DataSyncRound: 1,
		},
	}

	assert.False(t, supportPreload(ds))
	require.NoError(t, (&DatasetReconciler{}).reconcileJob(context.Background(), ds))
	assert.False(t, ds.Status.InProcessing)
}

func TestDatasetReconciler_reconcilePhaseManual(t *testing.T) {
	tests := []struct {
		name   string
		status datasetv1alpha1.DatasetStatus
		want   datasetv1alpha1.DatasetStatusPhase
	}{
		{
			name: "ready after PVC reconciliation succeeds",
			status: datasetv1alpha1.DatasetStatus{
				PVCName: "manual-dataset",
				Conditions: []metav1.Condition{{
					Type:   condTypePVC,
					Status: metav1.ConditionTrue,
				}},
			},
			want: datasetv1alpha1.DatasetStatusPhaseReady,
		},
		{
			name: "pending before PVC reconciliation succeeds",
			status: datasetv1alpha1.DatasetStatus{
				PVCName: "manual-dataset",
			},
			want: datasetv1alpha1.DatasetStatusPhasePending,
		},
		{
			name: "failed when reconciliation reports an error",
			status: datasetv1alpha1.DatasetStatus{
				PVCName: "manual-dataset",
				Conditions: []metav1.Condition{{
					Type:   condTypePVC,
					Status: metav1.ConditionFalse,
				}},
			},
			want: datasetv1alpha1.DatasetStatusPhaseFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds := &datasetv1alpha1.Dataset{
				Spec: datasetv1alpha1.DatasetSpec{
					Source: datasetv1alpha1.DatasetSource{
						Type: datasetv1alpha1.DatasetTypeManual,
						URI:  "manual://",
					},
					DataSyncRound: 1,
				},
				Status: tt.status,
			}

			require.NoError(t, (&DatasetReconciler{}).reconcilePhase(context.Background(), ds))
			assert.Equal(t, tt.want, ds.Status.Phase)
		})
	}
}

func TestDatasetReconciler_validateManualURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		wantErr bool
	}{
		{name: "canonical URI", uri: "manual://"},
		{name: "non-canonical URI", uri: "manual://invalid", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds := &datasetv1alpha1.Dataset{
				Spec: datasetv1alpha1.DatasetSpec{
					Source: datasetv1alpha1.DatasetSource{
						Type: datasetv1alpha1.DatasetTypeManual,
						URI:  tt.uri,
					},
				},
			}

			err := (&DatasetReconciler{}).validate(context.Background(), ds)
			if tt.wantErr {
				require.EqualError(t, err, "MANUAL dataset source URI must be manual://")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestDatasetReconciler_reconcileMountPolicyStoresEffectivePermissions(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, datasetv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	rootUID := types.UID("root-uid")
	rootPVCUID := types.UID("root-pvc-uid")
	rootPVUID := types.UID("root-pv-uid")
	refUID := types.UID("ref-uid")
	refPVCUID := types.UID("ref-pvc-uid")
	refPVUID := types.UID("ref-pv-uid")
	root := &datasetv1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "root", Namespace: "root", UID: rootUID},
		Spec: datasetv1alpha1.DatasetSpec{
			Share: true,
			ShareAccess: &datasetv1alpha1.ShareAccess{Rules: []datasetv1alpha1.ShareAccessRule{{
				NamespaceSelector: metav1.LabelSelector{MatchLabels: map[string]string{"workspace": "one"}},
				AccessMode:        datasetv1alpha1.AccessModeReadWrite,
			}}},
			Source: datasetv1alpha1.DatasetSource{Type: datasetv1alpha1.DatasetTypeManual, URI: "manual://"},
		},
		Status: datasetv1alpha1.DatasetStatus{PVCName: "root-pvc"},
	}
	rootPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "root-pvc", Namespace: "root", UID: rootPVCUID},
		Spec:       corev1.PersistentVolumeClaimSpec{VolumeName: "root-pv"},
		Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
	}
	rootPV := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "root-pv", UID: rootPVUID},
		Spec: corev1.PersistentVolumeSpec{
			ClaimRef:               &corev1.ObjectReference{Namespace: "root", Name: "root-pvc", UID: rootPVCUID},
			PersistentVolumeSource: corev1.PersistentVolumeSource{NFS: &corev1.NFSVolumeSource{Server: "nfs", Path: "/dataset"}},
		},
	}
	ref := &datasetv1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "ref", Namespace: "target", UID: refUID, Generation: 3},
		Spec: datasetv1alpha1.DatasetSpec{Source: datasetv1alpha1.DatasetSource{
			Type: datasetv1alpha1.DatasetTypeReference,
			URI:  "dataset://root/root",
		}},
		Status: datasetv1alpha1.DatasetStatus{PVCName: "ref-pvc"},
	}
	owner := *metav1.NewControllerRef(ref, datasetv1alpha1.GroupVersion.WithKind("Dataset"))
	refPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "ref-pvc", Namespace: "target", UID: refPVCUID, OwnerReferences: []metav1.OwnerReference{owner}},
		Spec:       corev1.PersistentVolumeClaimSpec{VolumeName: "ref-pv"},
		Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
	}
	refPV := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "ref-pv", UID: refPVUID, OwnerReferences: []metav1.OwnerReference{owner}, Annotations: map[string]string{
			mountpolicy.SourceDatasetUIDAnnotation: string(rootUID),
			mountpolicy.SourcePVCUIDAnnotation:     string(rootPVCUID),
			mountpolicy.SourcePVUIDAnnotation:      string(rootPVUID),
		}},
		Spec: corev1.PersistentVolumeSpec{
			ClaimRef:               &corev1.ObjectReference{Namespace: "target", Name: "ref-pvc", UID: refPVCUID},
			PersistentVolumeSource: corev1.PersistentVolumeSource{NFS: &corev1.NFSVolumeSource{Server: "nfs", Path: "/dataset"}},
		},
	}
	workspace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "target", Labels: map[string]string{"workspace": "one"}}}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(root, rootPVC, rootPV, refPVC, refPV, workspace).Build()
	reconciler := &DatasetReconciler{Client: fakeClient, Scheme: scheme}

	require.NoError(t, reconciler.reconcileMountPolicy(ctx, ref))
	reconciler.setMountPolicyCondition(ref, nil)
	require.False(t, ref.Status.ReadOnly)
	require.Equal(t, []datasetv1alpha1.MountSource{{Namespace: "root", Name: "root", UID: string(rootUID), PVCName: "root-pvc", PVCUID: string(rootPVCUID), PVName: "root-pv", PVUID: string(rootPVUID)}}, ref.Status.MountSources)
	require.True(t, kubeutils.IsConditionReady(ref.Status.Conditions, condTypeMountPolicy))
	require.Equal(t, int64(3), ref.Status.Conditions[0].ObservedGeneration)
}
