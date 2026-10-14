package dataset

import (
	"context"
	api "github.com/BaizeAI/dataset/api/dataset/v1alpha1"
	"github.com/BaizeAI/dataset/config"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"testing"
)

func TestNFSVersionPerDataset(t *testing.T) {
	for _, version := range []string{"3", "4.0", "4.1", "4.2", "", "5"} {
		t.Run(version, func(t *testing.T) {
			t.Setenv("DATASET_NFS_VERSION", "4.2")
			require.NoError(t, config.ParseConfigFromFileContent(""))
			scheme := runtime.NewScheme()
			require.NoError(t, api.AddToScheme(scheme))
			require.NoError(t, corev1.AddToScheme(scheme))
			ds := &api.Dataset{ObjectMeta: metav1.ObjectMeta{Name: "nfs", Namespace: "test"}, Spec: api.DatasetSpec{Source: api.DatasetSource{Type: api.DatasetTypeNFS, URI: "nfs://server/export", Options: map[string]string{"nfsVersion": version}}}}
			cli := fake.NewClientBuilder().WithScheme(scheme).Build()
			controller := &DatasetReconciler{Client: cli, Scheme: scheme}
			err := controller.reconcilePVC(context.Background(), ds)
			if version == "" || version == "5" {
				require.Error(t, err)
				pvs := &corev1.PersistentVolumeList{}
				require.NoError(t, cli.List(context.Background(), pvs))
				require.Empty(t, pvs.Items)
				return
			}
			require.NoError(t, err)
			pv := &corev1.PersistentVolume{}
			require.NoError(t, cli.Get(context.Background(), client.ObjectKey{Name: "dataset-test-pvc-nfs"}, pv))
			require.Equal(t, []string{"nfsvers=" + version}, pv.Spec.MountOptions)
			t.Setenv("DATASET_NFS_VERSION", "4.0")
			require.NoError(t, config.ParseConfigFromFileContent(""))
			require.NoError(t, controller.reconcilePVC(context.Background(), ds))
			require.NoError(t, cli.Get(context.Background(), client.ObjectKeyFromObject(pv), pv))
			require.Equal(t, []string{"nfsvers=" + version}, pv.Spec.MountOptions)
		})
	}
}

func TestNFSVersionDefensiveValidation(t *testing.T) {
	r := &DatasetReconciler{}
	for _, sourceType := range []api.DatasetType{api.DatasetTypeS3, api.DatasetTypeReference, api.DatasetTypePVC} {
		ds := &api.Dataset{Spec: api.DatasetSpec{Source: api.DatasetSource{Type: sourceType, Options: map[string]string{"nfsVersion": "3"}}}}
		require.ErrorContains(t, r.validate(context.Background(), ds), "only supported for NFS")
	}
	ds := &api.Dataset{Spec: api.DatasetSpec{Source: api.DatasetSource{Type: api.DatasetTypeNFS, Options: map[string]string{"nfsVersion": "3"}}, VolumeClaimRef: &api.VolumeClaimRef{Name: "external"}}}
	require.ErrorContains(t, r.validate(context.Background(), ds), "volumeClaimRef")
}
