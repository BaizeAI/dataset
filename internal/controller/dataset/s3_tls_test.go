package dataset

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	dsapi "github.com/BaizeAI/dataset/api/dataset/v1alpha1"
)

func TestS3TLSJob(t *testing.T) {
	for _, mode := range []string{"", "system", "customCA", "insecureSkipVerify"} {
		t.Run(mode, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, dsapi.AddToScheme(scheme))
			require.NoError(t, batchv1.AddToScheme(scheme))
			require.NoError(t, corev1.AddToScheme(scheme))
			// No Secret is installed in the fake client. Job creation must not read it.
			c := fake.NewClientBuilder().WithScheme(scheme).Build()
			r := &DatasetReconciler{Client: c}
			ds := &dsapi.Dataset{ObjectMeta: metav1.ObjectMeta{Name: "s3", Namespace: "test", UID: "uid"}, Spec: dsapi.DatasetSpec{DataSyncRound: 1, SecretRef: "credentials", Source: dsapi.DatasetSource{Type: dsapi.DatasetTypeS3, URI: "s3://bucket", Options: map[string]string{"tlsMode": mode, "tlsCAFile": "/untrusted/path"}}}}
			ds.Status.PVCName = "data"
			if mode == "customCA" {
				ds.Spec.Source.Options["tlsCASecretName"] = "ca"
			}
			before := ds.DeepCopy().Spec.Source.Options
			require.NoError(t, r.reconcileJob(context.Background(), ds))
			require.Equal(t, before, ds.Spec.Source.Options)
			jobs := &batchv1.JobList{}
			require.NoError(t, c.List(context.Background(), jobs))
			require.Len(t, jobs.Items, 1)
			pod := jobs.Items[0].Spec.Template.Spec
			container := pod.Containers[0]
			args := strings.Join(container.Args, " ")
			require.NotContains(t, args, "tlsCASecretName")
			require.NotContains(t, args, "/untrusted/path")
			if mode == "customCA" {
				require.Len(t, pod.Volumes, 3)
				ca := pod.Volumes[0].Secret
				require.NotNil(t, ca)
				require.Equal(t, "ca", ca.SecretName)
				require.Nil(t, ca.Optional)
				require.Equal(t, []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}}, ca.Items)
				mount := container.VolumeMounts[0]
				require.True(t, mount.ReadOnly)
				require.Empty(t, mount.SubPath)
				require.Equal(t, "/run/dataset/s3-tls", mount.MountPath)
				require.Contains(t, args, "--options=tlsCAFile=/run/dataset/s3-tls/ca.crt")
			} else {
				require.Len(t, pod.Volumes, 2)
				require.NotContains(t, args, "tlsCAFile")
			}
			require.Equal(t, "credentials", pod.Volumes[len(pod.Volumes)-2].Secret.SecretName)
			require.Equal(t, "data", pod.Volumes[len(pod.Volumes)-1].PersistentVolumeClaim.ClaimName)
		})
	}
}

func TestS3TLSJobRejectsInvalidReferences(t *testing.T) {
	for _, options := range []map[string]string{{"tlsMode": "unknown"}, {"tlsMode": "customCA"}, {"tlsMode": "system", "tlsCASecretName": "ca"}, {"tlsMode": "insecureSkipVerify", "tlsCASecretName": "ca"}} {
		pod := &corev1.PodSpec{}
		container := &corev1.Container{}
		require.Error(t, configureS3TLS(options, pod, container))
		require.Empty(t, pod.Volumes)
	}
}
