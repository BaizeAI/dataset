package dataset

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	datasetv1alpha1 "github.com/BaizeAI/dataset/api/dataset/v1alpha1"
	"github.com/BaizeAI/dataset/pkg/mountpolicy"
)

func TestReconcileMountPolicyStoresFullSourceChain(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, datasetv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	a := referenceTestSource("a", "a", "a-uid", "a-pvc", "a-pvc-uid", "a-pv", "a-pv-uid", nil, nil)
	a.Spec.Share = true
	b := referenceTestSource("b", "b", "b-uid", "b-pvc", "b-pvc-uid", "b-pv", "b-pv-uid", a, &datasetv1alpha1.DatasetSource{Type: datasetv1alpha1.DatasetTypeReference, URI: "dataset://a/a"})
	c := referenceTestSource("c", "target", "c-uid", "c-pvc", "c-pvc-uid", "c-pv", "c-pv-uid", b, &datasetv1alpha1.DatasetSource{Type: datasetv1alpha1.DatasetTypeReference, URI: "dataset://b/b"})
	c.Spec.Share = false
	objects := append(referenceTestStorage(a, nil), referenceTestStorage(b, a)...)
	objects = append(objects, referenceTestStorage(c, b)...)
	objects = append(objects, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "target", Labels: map[string]string{"workspace": "one"}}})
	reconciler := &DatasetReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build(), Scheme: scheme}

	require.NoError(t, reconciler.reconcileMountPolicy(ctx, c))
	reconciler.setMountPolicyCondition(c, nil)
	require.Equal(t, []datasetv1alpha1.MountSource{
		{Namespace: "b", Name: "b", UID: "b-uid", PVCName: "b-pvc", PVCUID: "b-pvc-uid", PVName: "b-pv", PVUID: "b-pv-uid"},
		{Namespace: "a", Name: "a", UID: "a-uid", PVCName: "a-pvc", PVCUID: "a-pvc-uid", PVName: "a-pv", PVUID: "a-pv-uid"},
	}, c.Status.MountSources)
	require.False(t, c.Status.ReadOnly)
}

func TestReferenceFailedRequeuesWithoutWatchEvent(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, datasetv1alpha1.AddToScheme(scheme))
	ref := &datasetv1alpha1.Dataset{ObjectMeta: metav1.ObjectMeta{Name: "waiting", Namespace: "target"}, Spec: datasetv1alpha1.DatasetSpec{Source: datasetv1alpha1.DatasetSource{Type: datasetv1alpha1.DatasetTypeReference, URI: "dataset://origin/missing"}}}
	reconciler := &DatasetReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(ref).WithObjects(ref).Build(), Scheme: scheme}

	result, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(ref)})
	require.NoError(t, err)
	require.Equal(t, 30*time.Second, result.RequeueAfter)
}

func TestReferencePVNameAcceptsShortUID(t *testing.T) {
	require.Equal(t, "dataset-team-data-short", referencePVName(&datasetv1alpha1.Dataset{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "data", UID: types.UID("short")}}))
	require.Equal(t, "dataset-team-data-pending", referencePVName(&datasetv1alpha1.Dataset{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "data"}}))
}

func referenceTestSource(name, namespace, uid, pvcName, pvcUID, pvName, pvUID string, parent *datasetv1alpha1.Dataset, source *datasetv1alpha1.DatasetSource) *datasetv1alpha1.Dataset {
	dsSource := datasetv1alpha1.DatasetSource{Type: datasetv1alpha1.DatasetTypeManual, URI: "manual://"}
	if source != nil {
		dsSource = *source
	}
	return &datasetv1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: types.UID(uid)},
		Spec: datasetv1alpha1.DatasetSpec{
			Share:       parent != nil,
			ShareAccess: &datasetv1alpha1.ShareAccess{Rules: []datasetv1alpha1.ShareAccessRule{{NamespaceSelector: metav1.LabelSelector{MatchLabels: map[string]string{"workspace": "one"}}, AccessMode: datasetv1alpha1.AccessModeReadWrite}}},
			Source:      dsSource,
		},
		Status: datasetv1alpha1.DatasetStatus{PVCName: pvcName},
	}
}

func referenceTestStorage(ds, parent *datasetv1alpha1.Dataset) []client.Object {
	pvcUID := types.UID(ds.Name + "-pvc-uid")
	pvUID := types.UID(ds.Name + "-pv-uid")
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: ds.Status.PVCName, Namespace: ds.Namespace, UID: pvcUID}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: ds.Name + "-pv"}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}
	pv := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: ds.Name + "-pv", UID: pvUID}, Spec: corev1.PersistentVolumeSpec{ClaimRef: &corev1.ObjectReference{Namespace: ds.Namespace, Name: pvc.Name, UID: pvc.UID}, PersistentVolumeSource: corev1.PersistentVolumeSource{NFS: &corev1.NFSVolumeSource{Server: "nfs", Path: "/data"}}}}
	if parent != nil {
		owner := *metav1.NewControllerRef(ds, datasetv1alpha1.GroupVersion.WithKind("Dataset"))
		pvc.OwnerReferences = []metav1.OwnerReference{owner}
		pv.OwnerReferences = []metav1.OwnerReference{owner}
		pv.Annotations = map[string]string{
			mountpolicy.SourceDatasetUIDAnnotation: string(parent.UID),
			mountpolicy.SourcePVCUIDAnnotation:     parent.Name + "-pvc-uid",
			mountpolicy.SourcePVUIDAnnotation:      parent.Name + "-pv-uid",
		}
	}
	return []client.Object{ds, pvc, pv}
}
