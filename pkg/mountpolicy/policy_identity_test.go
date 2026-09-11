package mountpolicy

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	datasetv1alpha1 "github.com/BaizeAI/dataset/api/dataset/v1alpha1"
)

func TestVerifyRejectsRecreatedSourceIdentities(t *testing.T) {
	ctx := context.Background()
	rootUID, rootPVCUID, rootPVUID := types.UID("root"), types.UID("root-pvc"), types.UID("root-pv")
	leafUID, leafPVCUID, leafPVUID := types.UID("leaf"), types.UID("leaf-pvc"), types.UID("leaf-pv")
	target := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "target", Labels: map[string]string{"workspace": "one"}}}
	root := &datasetv1alpha1.Dataset{ObjectMeta: metav1.ObjectMeta{Name: "root", Namespace: "root", UID: rootUID}, Spec: datasetv1alpha1.DatasetSpec{Share: true, ShareAccess: policy(datasetv1alpha1.AccessModeReadWrite, "one"), Source: datasetv1alpha1.DatasetSource{Type: datasetv1alpha1.DatasetTypeManual, URI: "manual://"}}, Status: datasetv1alpha1.DatasetStatus{PVCName: "root-pvc"}}
	rootPVC := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "root-pvc", Namespace: "root", UID: rootPVCUID}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "root-pv"}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}
	rootPV := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "root-pv", UID: rootPVUID}, Spec: corev1.PersistentVolumeSpec{ClaimRef: &corev1.ObjectReference{Namespace: "root", Name: "root-pvc", UID: rootPVCUID}, PersistentVolumeSource: corev1.PersistentVolumeSource{NFS: &corev1.NFSVolumeSource{Server: "nfs", Path: "/data"}}}}
	leaf := &datasetv1alpha1.Dataset{ObjectMeta: metav1.ObjectMeta{Name: "leaf", Namespace: "target", UID: leafUID, Generation: 1}, Spec: datasetv1alpha1.DatasetSpec{Source: datasetv1alpha1.DatasetSource{Type: datasetv1alpha1.DatasetTypeReference, URI: "dataset://root/root"}}, Status: datasetv1alpha1.DatasetStatus{PVCName: "leaf-pvc", MountSources: []datasetv1alpha1.MountSource{{Namespace: "root", Name: "root", UID: string(rootUID), PVCName: "root-pvc", PVCUID: string(rootPVCUID), PVName: "root-pv", PVUID: string(rootPVUID)}}, Conditions: []metav1.Condition{{Type: MountPolicyCondition, Status: metav1.ConditionTrue, ObservedGeneration: 1}}}}
	owner := *metav1.NewControllerRef(leaf, datasetv1alpha1.GroupVersion.WithKind("Dataset"))
	leafPVC := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "leaf-pvc", Namespace: "target", UID: leafPVCUID, OwnerReferences: []metav1.OwnerReference{owner}}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "leaf-pv"}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}
	leafPV := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "leaf-pv", UID: leafPVUID, OwnerReferences: []metav1.OwnerReference{owner}, Annotations: map[string]string{SourceDatasetUIDAnnotation: string(rootUID), SourcePVCUIDAnnotation: string(rootPVCUID), SourcePVUIDAnnotation: string(rootPVUID)}}, Spec: corev1.PersistentVolumeSpec{ClaimRef: &corev1.ObjectReference{Namespace: "target", Name: "leaf-pvc", UID: leafPVCUID}, PersistentVolumeSource: corev1.PersistentVolumeSource{NFS: &corev1.NFSVolumeSource{Server: "nfs", Path: "/data"}}}}

	_, err := Verify(ctx, newReader(t, target, root, rootPVC, rootPV, leafPVC, leafPV), leaf)
	require.NoError(t, err)

	tests := []struct {
		name    string
		objects func() []client.Object
	}{
		{
			name: "dataset UID",
			objects: func() []client.Object {
				rebuilt := root.DeepCopy()
				rebuilt.UID = types.UID("root-rebuilt")
				return []client.Object{target, rebuilt, rootPVC, rootPV, leafPVC, leafPV}
			},
		},
		{
			name: "PVC UID",
			objects: func() []client.Object {
				rebuiltPVC := rootPVC.DeepCopy()
				rebuiltPVC.UID = types.UID("root-pvc-rebuilt")
				rebuiltPV := rootPV.DeepCopy()
				rebuiltPV.Spec.ClaimRef.UID = rebuiltPVC.UID
				return []client.Object{target, root, rebuiltPVC, rebuiltPV, leafPVC, leafPV}
			},
		},
		{
			name: "PV UID",
			objects: func() []client.Object {
				rebuilt := rootPV.DeepCopy()
				rebuilt.UID = types.UID("root-pv-rebuilt")
				return []client.Object{target, root, rootPVC, rebuilt, leafPVC, leafPV}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Verify(ctx, newReader(t, tt.objects()...), leaf)
			require.ErrorContains(t, err, "mount source bindings no longer match status")
		})
	}
}
