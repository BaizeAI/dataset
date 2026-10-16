package dataset

import (
	"context"
	"testing"

	api "github.com/BaizeAI/dataset/api/dataset/v1alpha1"
	"github.com/BaizeAI/dataset/config"
	"github.com/BaizeAI/dataset/internal/pkg/constants"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestReconcilePVCNFSURI(t *testing.T) {
	tests := []struct {
		name, uri, server, port, host, path, version string
	}{
		{name: "reported IPv4 port", uri: "nfs://10.0.6.23:20490/exports", server: "10.0.6.23", port: "20490", host: "10.0.6.23:20490", path: "/exports", version: "4.0"},
		{name: "IPv4 default", uri: "nfs://192.0.2.1/exports", server: "192.0.2.1", host: "192.0.2.1", path: "/exports"},
		{name: "DNS default", uri: "nfs://nfs.example.test/exports", server: "nfs.example.test", host: "nfs.example.test", path: "/exports"},
		{name: "DNS port NFS 3", uri: "nfs://nfs.example.test:32049/exports", server: "nfs.example.test", port: "32049", host: "nfs.example.test:32049", path: "/exports", version: "3"},
		{name: "explicit standard port NFS 4.1", uri: "nfs://nfs.example.test:2049/exports", server: "nfs.example.test", port: "2049", host: "nfs.example.test:2049", path: "/exports", version: "4.1"},
		{name: "maximum port NFS 4.2", uri: "nfs://192.0.2.1:65535/exports", server: "192.0.2.1", port: "65535", host: "192.0.2.1:65535", path: "/exports", version: "4.2"},
		{name: "minimum port", uri: "nfs://nfs.example.test:1/exports", server: "nfs.example.test", port: "1", host: "nfs.example.test:1", path: "/exports"},
		{name: "decimal port with leading zeros", uri: "nfs://nfs.example.test:032049/exports", server: "nfs.example.test", port: "32049", host: "nfs.example.test:032049", path: "/exports"},
		{name: "IPv6 zone default", uri: "nfs://[fe80::1%25eth0]/exports", server: "[fe80::1%eth0]", host: "[fe80::1%eth0]", path: "/exports"},
		{name: "IPv6 zone port", uri: "nfs://[fe80::1%25eth0]:32049/exports", server: "[fe80::1%eth0]", port: "32049", host: "[fe80::1%eth0]:32049", path: "/exports"},
		{name: "IPv6 default", uri: "nfs://[2001:db8::1]/exports", server: "[2001:db8::1]", host: "[2001:db8::1]", path: "/exports"},
		{name: "IPv6 port", uri: "nfs://[2001:db8::1]:32049/exports", server: "[2001:db8::1]", port: "32049", host: "[2001:db8::1]:32049", path: "/exports"},
		{name: "legacy unbracketed IPv6", uri: "nfs://2001:db8::1/exports", server: "2001:db8::1", host: "2001:db8::1", path: "/exports"},
		{name: "legacy IPv6 numeric suffix is not a port", uri: "nfs://2001:db8::2049/exports", server: "2001:db8::2049", host: "2001:db8::2049", path: "/exports"},
		{name: "nested directory", uri: "nfs://nfs.example.test:32049/exports/data/", server: "nfs.example.test", port: "32049", host: "nfs.example.test:32049", path: "/exports/data/"},
		{name: "escaped directory", uri: "nfs://nfs.example.test:32049/exports/my%20data", server: "nfs.example.test", port: "32049", host: "nfs.example.test:32049", path: "/exports/my data"},
		{name: "root", uri: "nfs://nfs.example.test:32049/", server: "nfs.example.test", port: "32049", host: "nfs.example.test:32049", path: "/"},
		{name: "empty directory", uri: "nfs://nfs.example.test", server: "nfs.example.test", host: "nfs.example.test"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, ds := newNFSURIReconciler(t, tt.uri)
			version := "4.0"
			if tt.version != "" {
				ds.Spec.Source.Options = map[string]string{"nfsVersion": tt.version}
				version = tt.version
			}
			require.NoError(t, r.reconcilePVC(context.Background(), ds))
			pv := &corev1.PersistentVolume{}
			require.NoError(t, r.Get(context.Background(), client.ObjectKey{Name: "dataset-test-pvc-nfs"}, pv))
			options := []string{"nfsvers=" + version}
			if tt.port != "" {
				options = append(options, "port="+tt.port)
			}
			// A URI's NFS port must not imply mountd, lockd, or other service
			// options, even for NFS 3.
			require.Equal(t, options, pv.Spec.MountOptions)
			require.Equal(t, tt.server, pv.Spec.CSI.VolumeAttributes["server"])
			require.Equal(t, "/", pv.Spec.CSI.VolumeAttributes["share"])
			require.Equal(t, tt.path, pv.Spec.CSI.VolumeAttributes["subdir"])
			require.Equal(t, "retain", pv.Spec.CSI.VolumeAttributes["onDelete"])
			require.Equal(t, tt.host+"#"+tt.path+"#"+pv.Name+"#", pv.Spec.CSI.VolumeHandle)
			pvc := &corev1.PersistentVolumeClaim{}
			require.NoError(t, r.Get(context.Background(), client.ObjectKeyFromObject(ds), pvc))
			require.Equal(t, pv.Name, pvc.Spec.VolumeName)
			originalSpec := pv.Spec.DeepCopy()
			require.NoError(t, r.reconcilePVC(context.Background(), ds))
			require.NoError(t, r.Get(context.Background(), client.ObjectKeyFromObject(pv), pv))
			require.Equal(t, *originalSpec, pv.Spec)
		})
	}
}

func TestReconcilePVCNFSURIInvalid(t *testing.T) {
	for _, uri := range []string{
		"nfs://server:invalid/exports",
		"nfs://server:-1/exports",
		"nfs://server:0/exports",
		"nfs://server:65536/exports",
		"nfs://server:999999999999999999999999/exports",
		"nfs://server:/exports",
		"nfs://[2001:db8::1]:0/exports",
		"nfs://[2001:db8::1]:65536/exports",
		"nfs://[2001:db8::1]:/exports",
		"nfs://server:123:456/exports",
		"nfs:///exports",
		"nfs://:32049/exports",
		"nfs://[]/exports",
		"%",
	} {
		t.Run(uri, func(t *testing.T) {
			r, ds := newNFSURIReconciler(t, uri)
			require.Error(t, r.reconcilePVC(context.Background(), ds))
			pvs := &corev1.PersistentVolumeList{}
			require.NoError(t, r.List(context.Background(), pvs))
			require.Empty(t, pvs.Items)
			pvcs := &corev1.PersistentVolumeClaimList{}
			require.NoError(t, r.List(context.Background(), pvcs))
			require.Empty(t, pvcs.Items)
		})
	}
}

func TestReconcilePVCNFSURIDoesNotRewriteExistingPV(t *testing.T) {
	r, ds := newNFSURIReconciler(t, "nfs://10.0.6.23:20490/exports")
	// Correcting an old PV's immutable CSI source requires an explicit
	// replacement by the operator.
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "dataset-test-pvc-nfs",
			Labels: map[string]string{constants.DatasetNameLabel: ds.Name},
		},
		Spec: corev1.PersistentVolumeSpec{
			MountOptions: []string{"nfsvers=4.0"},
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				CSI: &corev1.CSIPersistentVolumeSource{
					Driver:       "nfs.csi.k8s.io",
					VolumeHandle: "10.0.6.23:20490#/exports#dataset-test-pvc-nfs#",
					VolumeAttributes: map[string]string{
						"server": "10.0.6.23:20490", "share": "/", "subdir": "/exports",
					},
				},
			},
		},
	}
	require.NoError(t, r.Create(context.Background(), pv))
	originalSpec := pv.Spec.DeepCopy()
	require.NoError(t, r.reconcilePVC(context.Background(), ds))
	stored := &corev1.PersistentVolume{}
	require.NoError(t, r.Get(context.Background(), client.ObjectKeyFromObject(pv), stored))
	require.Equal(t, *originalSpec, stored.Spec)
}

func newNFSURIReconciler(t *testing.T, uri string) (*DatasetReconciler, *api.Dataset) {
	t.Helper()
	t.Setenv("DATASET_NFS_VERSION", "4.0")
	require.NoError(t, config.ParseConfigFromFileContent(""))
	scheme := runtime.NewScheme()
	require.NoError(t, api.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	ds := &api.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "nfs", Namespace: "test"},
		Spec: api.DatasetSpec{Source: api.DatasetSource{
			Type: api.DatasetTypeNFS, URI: uri,
		}},
	}
	return &DatasetReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).Build(), Scheme: scheme}, ds
}
