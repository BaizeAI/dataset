package mountpolicy

import (
	"context"
	"fmt"
	"testing"

	datasetv1alpha1 "github.com/BaizeAI/dataset/api/dataset/v1alpha1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func resolutionChain(depth int, distinctNamespaces bool) (*datasetv1alpha1.Dataset, []client.Object) {
	var objects []client.Object
	var previous *datasetv1alpha1.Dataset
	for i := 0; i <= depth; i++ {
		namespace := "shared"
		if distinctNamespaces {
			namespace = fmt.Sprintf("ns-%d", i)
		}
		if i == 0 || distinctNamespaces {
			objects = append(objects, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
				Name: namespace, Labels: map[string]string{"workspace": "one"},
			}})
		}
		ds := &datasetv1alpha1.Dataset{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: fmt.Sprintf("dataset-%d", i)},
			Spec: datasetv1alpha1.DatasetSpec{
				Share: true, ShareAccess: policy(datasetv1alpha1.AccessModeReadWrite, "one"),
				Source: datasetv1alpha1.DatasetSource{Type: datasetv1alpha1.DatasetTypeManual},
			},
		}
		if previous != nil {
			ds.Spec.Source = datasetv1alpha1.DatasetSource{
				Type: datasetv1alpha1.DatasetTypeReference,
				URI:  fmt.Sprintf("dataset://%s/%s", previous.Namespace, previous.Name),
			}
		}
		objects = append(objects, ds)
		previous = ds
	}
	return previous, objects
}

func TestResolveMaximumDepthNamespaces(t *testing.T) {
	for _, distinct := range []bool{false, true} {
		t.Run(fmt.Sprintf("distinct=%t", distinct), func(t *testing.T) {
			leaf, objects := resolutionChain(MaxReferenceDepth, distinct)
			resolution, err := Resolve(context.Background(), newReader(t, objects...), leaf)
			require.NoError(t, err)
			require.Len(t, resolution.Sources, MaxReferenceDepth)
			require.False(t, resolution.ReadOnly)
		})
	}
}

func TestGrantValidatesAllRulesBeforeMatching(t *testing.T) {
	source := &datasetv1alpha1.Dataset{Spec: datasetv1alpha1.DatasetSpec{
		Share: true, ShareAccess: policy(datasetv1alpha1.AccessModeReadOnly, "one"),
	}}
	source.Spec.ShareAccess.Rules = append(source.Spec.ShareAccess.Rules, datasetv1alpha1.ShareAccessRule{
		AccessMode: datasetv1alpha1.AccessModeReadWrite,
		NamespaceSelector: metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
			Key: "workspace", Operator: metav1.LabelSelectorOperator("invalid"),
		}}},
	})
	reader := newReader(t, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: "target", Labels: map[string]string{"workspace": "one"},
	}})
	grant, err := Grant(context.Background(), reader, source, "target")
	require.ErrorContains(t, err, "shareAccess.rules[1].namespaceSelector is invalid")
	require.Equal(t, Denied, grant)
}

func BenchmarkResolveNamespaces(b *testing.B) {
	for _, distinct := range []bool{false, true} {
		b.Run(fmt.Sprintf("distinct=%t", distinct), func(b *testing.B) {
			leaf, objects := resolutionChain(MaxReferenceDepth, distinct)
			// Use the same fake API reader for both layouts and all iterations.
			scheme := runtime.NewScheme()
			require.NoError(b, datasetv1alpha1.AddToScheme(scheme))
			require.NoError(b, corev1.AddToScheme(scheme))
			reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := Resolve(context.Background(), reader, leaf); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
