package v1alpha1_test

import (
	"context"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	datasetv1alpha1 "github.com/BaizeAI/dataset/api/dataset/v1alpha1"
)

func TestShareAccessCRDValidation(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("envtest process cleanup is unsupported on Windows; run this test on Linux CI")
	}
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS is required for envtest")
	}
	_, file, _, ok := goruntime.Caller(0)
	require.True(t, ok)
	crdPath := filepath.Join(filepath.Dir(file), "..", "..", "..", "config", "crd", "bases")
	testEnv := &envtest.Environment{CRDDirectoryPaths: []string{crdPath}, ErrorIfCRDPathMissing: true}
	cfg, err := testEnv.Start()
	require.NoError(t, err)
	defer func() { require.NoError(t, testEnv.Stop()) }()

	scheme := runtime.NewScheme()
	require.NoError(t, datasetv1alpha1.AddToScheme(scheme))
	apiClient, err := client.New(cfg, client.Options{Scheme: scheme})
	require.NoError(t, err)
	ctx := context.Background()

	validRule := datasetv1alpha1.ShareAccessRule{
		NamespaceSelector: metav1.LabelSelector{MatchLabels: map[string]string{"workspace": "one"}},
		AccessMode:        datasetv1alpha1.AccessModeReadOnly,
	}
	newDataset := func(name string) *datasetv1alpha1.Dataset {
		return &datasetv1alpha1.Dataset{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec:       datasetv1alpha1.DatasetSpec{Source: datasetv1alpha1.DatasetSource{Type: datasetv1alpha1.DatasetTypeManual, URI: "manual://"}},
		}
	}

	t.Run("rejects explicit empty rules even before sharing is enabled", func(t *testing.T) {
		ds := newDataset("empty-rules")
		ds.Spec.ShareAccess = &datasetv1alpha1.ShareAccess{}
		require.Error(t, apiClient.Create(ctx, ds))
	})
	t.Run("rejects empty namespace selector", func(t *testing.T) {
		ds := newDataset("empty-selector")
		ds.Spec.ShareAccess = &datasetv1alpha1.ShareAccess{Rules: []datasetv1alpha1.ShareAccessRule{{AccessMode: datasetv1alpha1.AccessModeReadOnly}}}
		require.Error(t, apiClient.Create(ctx, ds))
	})
	t.Run("allows a nonempty preconfigured policy and later enabling sharing", func(t *testing.T) {
		ds := newDataset("preconfigured")
		ds.Spec.ShareAccess = &datasetv1alpha1.ShareAccess{Rules: []datasetv1alpha1.ShareAccessRule{validRule}}
		require.NoError(t, apiClient.Create(ctx, ds))
		ds.Spec.Share = true
		require.NoError(t, apiClient.Update(ctx, ds))
	})
	t.Run("rejects adding or changing a policy after creation", func(t *testing.T) {
		missing := newDataset("missing-policy")
		require.NoError(t, apiClient.Create(ctx, missing))
		missing.Spec.ShareAccess = &datasetv1alpha1.ShareAccess{Rules: []datasetv1alpha1.ShareAccessRule{validRule}}
		require.Error(t, apiClient.Update(ctx, missing))

		immutable := newDataset("immutable-policy")
		immutable.Spec.ShareAccess = &datasetv1alpha1.ShareAccess{Rules: []datasetv1alpha1.ShareAccessRule{validRule}}
		require.NoError(t, apiClient.Create(ctx, immutable))
		immutable.Spec.ShareAccess.Rules[0].AccessMode = datasetv1alpha1.AccessModeReadWrite
		require.Error(t, apiClient.Update(ctx, immutable))
	})
}
