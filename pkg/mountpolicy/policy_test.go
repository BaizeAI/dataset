package mountpolicy

import (
	"context"
	"testing"

	datasetv1alpha1 "github.com/BaizeAI/dataset/api/dataset/v1alpha1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func policy(mode datasetv1alpha1.AccessMode, value string) *datasetv1alpha1.ShareAccess {
	return &datasetv1alpha1.ShareAccess{Rules: []datasetv1alpha1.ShareAccessRule{{
		NamespaceSelector: metav1.LabelSelector{MatchLabels: map[string]string{"workspace": value}},
		AccessMode:        mode,
	}}}
}

func newReader(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, datasetv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func TestGrantRulesAndLegacyPolicy(t *testing.T) {
	ctx := context.Background()
	source := &datasetv1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "source", Namespace: "source"},
		Spec: datasetv1alpha1.DatasetSpec{Share: true, ShareAccess: &datasetv1alpha1.ShareAccess{Rules: []datasetv1alpha1.ShareAccessRule{
			{NamespaceSelector: metav1.LabelSelector{MatchLabels: map[string]string{"workspace": "rw"}}, AccessMode: datasetv1alpha1.AccessModeReadWrite},
			{NamespaceSelector: metav1.LabelSelector{MatchLabels: map[string]string{"workspace": "ro"}}, AccessMode: datasetv1alpha1.AccessModeReadOnly},
			// A readonly match wins over a simultaneously matching readwrite rule.
			{NamespaceSelector: metav1.LabelSelector{MatchLabels: map[string]string{"workspace": "rw", "tier": "restricted"}}, AccessMode: datasetv1alpha1.AccessModeReadOnly},
		}}},
	}
	rw := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "rw", Labels: map[string]string{"workspace": "rw"}}}
	restricted := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "restricted", Labels: map[string]string{"workspace": "rw", "tier": "restricted"}}}
	ro := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ro", Labels: map[string]string{"workspace": "ro"}}}
	denied := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "denied", Labels: map[string]string{"workspace": "other"}}}
	reader := newReader(t, rw, restricted, ro, denied)

	result, err := Grant(ctx, reader, source, "rw")
	require.NoError(t, err)
	require.Equal(t, ReadWrite, result)
	result, err = Grant(ctx, reader, source, "restricted")
	require.NoError(t, err)
	require.Equal(t, ReadOnly, result)
	result, err = Grant(ctx, reader, source, "ro")
	require.NoError(t, err)
	require.Equal(t, ReadOnly, result)
	result, err = Grant(ctx, reader, source, "denied")
	require.NoError(t, err)
	require.Equal(t, Denied, result)

	legacy := source.DeepCopy()
	legacy.Spec.ShareAccess = nil
	result, err = Grant(ctx, reader, legacy, "denied")
	require.NoError(t, err)
	require.Equal(t, ReadOnly, result)

	empty := source.DeepCopy()
	empty.Spec.ShareAccess = &datasetv1alpha1.ShareAccess{}
	_, err = Grant(ctx, reader, empty, "rw")
	require.ErrorContains(t, err, "must contain at least one rule")

	preconfigured := source.DeepCopy()
	preconfigured.Spec.Share = false
	preconfigured.Spec.ShareAccess = &datasetv1alpha1.ShareAccess{}
	require.ErrorContains(t, Validate(preconfigured), "must contain at least one rule")
}

func TestParseReference(t *testing.T) {
	key, err := ParseReference("dataset://source/data")
	require.NoError(t, err)
	require.Equal(t, client.ObjectKey{Namespace: "source", Name: "data"}, key)

	for _, uri := range []string{"https://source/data", "dataset://source/data/child", "dataset://source/data?x=y", "dataset://source/data#fragment"} {
		_, err := ParseReference(uri)
		require.Error(t, err, uri)
	}
}

func TestResolveRequiresEverySourceAndReadonlyWins(t *testing.T) {
	ctx := context.Background()
	target := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "target", Labels: map[string]string{"workspace": "one"}}}
	root := &datasetv1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "root", Namespace: "root", UID: types.UID("root-uid")},
		Spec:       datasetv1alpha1.DatasetSpec{Share: true, ShareAccess: policy(datasetv1alpha1.AccessModeReadOnly, "one"), Source: datasetv1alpha1.DatasetSource{Type: datasetv1alpha1.DatasetTypeManual, URI: "manual://"}},
	}
	middle := &datasetv1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "middle", Namespace: "middle", UID: types.UID("middle-uid")},
		Spec:       datasetv1alpha1.DatasetSpec{Share: true, ShareAccess: policy(datasetv1alpha1.AccessModeReadWrite, "one"), Source: datasetv1alpha1.DatasetSource{Type: datasetv1alpha1.DatasetTypeReference, URI: "dataset://root/root"}},
	}
	leaf := &datasetv1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "leaf", Namespace: "target"},
		Spec:       datasetv1alpha1.DatasetSpec{Source: datasetv1alpha1.DatasetSource{Type: datasetv1alpha1.DatasetTypeReference, URI: "dataset://middle/middle"}},
	}
	reader := newReader(t, target, root, middle)
	resolution, err := Resolve(ctx, reader, leaf)
	require.NoError(t, err)
	require.True(t, resolution.ReadOnly)
	require.Len(t, resolution.Sources, 2)
	require.Equal(t, []string{"middle", "root"}, []string{resolution.Sources[0].Name, resolution.Sources[1].Name})

	root.Spec.ShareAccess = policy(datasetv1alpha1.AccessModeReadWrite, "other")
	reader = newReader(t, target, root, middle)
	_, err = Resolve(ctx, reader, leaf)
	require.ErrorContains(t, err, "not shared")
}

func TestVerifyRequiresCurrentMountPolicyAndPinnedBindings(t *testing.T) {
	ctx := context.Background()
	rootUID, rootPVCUID, rootPVUID := types.UID("root"), types.UID("root-pvc"), types.UID("root-pv")
	leafUID, leafPVCUID, leafPVUID := types.UID("leaf"), types.UID("leaf-pvc"), types.UID("leaf-pv")
	target := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "target", Labels: map[string]string{"workspace": "one"}}}
	root := &datasetv1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "root", Namespace: "root", UID: rootUID},
		Spec:       datasetv1alpha1.DatasetSpec{Share: true, ShareAccess: policy(datasetv1alpha1.AccessModeReadWrite, "one"), Source: datasetv1alpha1.DatasetSource{Type: datasetv1alpha1.DatasetTypeManual, URI: "manual://"}},
		Status:     datasetv1alpha1.DatasetStatus{PVCName: "root-pvc"},
	}
	rootPVC := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "root-pvc", Namespace: "root", UID: rootPVCUID}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "root-pv"}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}
	rootPV := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "root-pv", UID: rootPVUID}, Spec: corev1.PersistentVolumeSpec{ClaimRef: &corev1.ObjectReference{Namespace: "root", Name: "root-pvc", UID: rootPVCUID}, PersistentVolumeSource: corev1.PersistentVolumeSource{NFS: &corev1.NFSVolumeSource{Server: "nfs", Path: "/data"}}}}
	leaf := &datasetv1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "leaf", Namespace: "target", UID: leafUID, Generation: 7},
		Spec:       datasetv1alpha1.DatasetSpec{Source: datasetv1alpha1.DatasetSource{Type: datasetv1alpha1.DatasetTypeReference, URI: "dataset://root/root"}},
		Status: datasetv1alpha1.DatasetStatus{
			PVCName:      "leaf-pvc",
			ReadOnly:     false,
			MountSources: []datasetv1alpha1.MountSource{{Namespace: "root", Name: "root", UID: string(rootUID), PVCName: "root-pvc", PVCUID: string(rootPVCUID), PVName: "root-pv", PVUID: string(rootPVUID)}},
			Conditions:   []metav1.Condition{{Type: MountPolicyCondition, Status: metav1.ConditionTrue, Reason: "MountPolicyResolved", Message: "", ObservedGeneration: 7}},
		},
	}
	owner := *metav1.NewControllerRef(leaf, datasetv1alpha1.GroupVersion.WithKind("Dataset"))
	leafPVC := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "leaf-pvc", Namespace: "target", UID: leafPVCUID, OwnerReferences: []metav1.OwnerReference{owner}}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "leaf-pv"}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}
	leafPV := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "leaf-pv", UID: leafPVUID, OwnerReferences: []metav1.OwnerReference{owner}, Annotations: map[string]string{
		SourceDatasetUIDAnnotation: string(rootUID), SourcePVCUIDAnnotation: string(rootPVCUID), SourcePVUIDAnnotation: string(rootPVUID),
	}}, Spec: corev1.PersistentVolumeSpec{
		ClaimRef:               &corev1.ObjectReference{Namespace: "target", Name: "leaf-pvc", UID: leafPVCUID},
		PersistentVolumeSource: corev1.PersistentVolumeSource{NFS: &corev1.NFSVolumeSource{Server: "nfs", Path: "/data"}},
	}}
	reader := newReader(t, target, root, rootPVC, rootPV, leafPVC, leafPV)
	_, err := Verify(ctx, reader, leaf)
	require.NoError(t, err)

	leaf.Status.Conditions[0].ObservedGeneration = 6
	_, err = Verify(ctx, reader, leaf)
	require.ErrorContains(t, err, "not ready")
}

func TestProtectedPVC(t *testing.T) {
	ref := &datasetv1alpha1.Dataset{ObjectMeta: metav1.ObjectMeta{Name: "ref", Namespace: "team"}, Spec: datasetv1alpha1.DatasetSpec{Source: datasetv1alpha1.DatasetSource{Type: datasetv1alpha1.DatasetTypeReference}, VolumeClaimTemplate: corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "managed"}}}}
	reader := newReader(t, ref)
	protected, err := ProtectedPVC(context.Background(), reader, "team", "managed")
	require.NoError(t, err)
	require.True(t, protected)
}
