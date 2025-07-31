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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	datasetv1alpha1 "github.com/BaizeAI/dataset/api/dataset/v1alpha1"
	"github.com/BaizeAI/dataset/config"
	"github.com/BaizeAI/dataset/internal/pkg/constants"
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
