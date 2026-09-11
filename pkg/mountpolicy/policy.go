// Package mountpolicy resolves and verifies Dataset REFERENCE mount permissions.
//
// It is intentionally independent from the Dataset controller so every mount
// consumer can make the same fail-closed decision immediately before creating a
// Pod.
package mountpolicy

import (
	"context"
	"fmt"
	"net/url"
	"reflect"
	"strings"

	datasetv1alpha1 "github.com/BaizeAI/dataset/api/dataset/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// MaxReferenceDepth bounds both the work performed by a request and the
	// amount of source state a Dataset status can retain.
	MaxReferenceDepth = 32
	// MountPolicyCondition is the authoritative condition for status.ReadOnly.
	MountPolicyCondition = "MountPolicy"

	SourceDatasetUIDAnnotation = "dataset.baizeai.io/mount-source-dataset-uid"
	SourcePVCUIDAnnotation     = "dataset.baizeai.io/mount-source-pvc-uid"
	SourcePVUIDAnnotation      = "dataset.baizeai.io/mount-source-pv-uid"
)

// GrantResult is the result of granting one source Dataset to one target
// namespace. It is not a Kubernetes volume access mode.
type GrantResult string

const (
	Denied    GrantResult = "Denied"
	ReadOnly  GrantResult = "ReadOnly"
	ReadWrite GrantResult = "ReadWrite"
)

// Resolution is the direct-to-root source chain for one REFERENCE Dataset.
type Resolution struct {
	Sources  []*datasetv1alpha1.Dataset
	ReadOnly bool
}

// Validate verifies policy data defensively. CRD CEL performs the same checks
// for normal API writes, but controllers must not trust objects that bypassed
// schema validation (for example, a fake client, an old API server, or an
// imported object).
func Validate(ds *datasetv1alpha1.Dataset) error {
	if ds == nil {
		return fmt.Errorf("dataset is required")
	}
	if ds.Spec.Source.Type == datasetv1alpha1.DatasetTypeReference && ds.Spec.VolumeClaimRef != nil {
		return fmt.Errorf("REFERENCE dataset cannot set volumeClaimRef")
	}
	if ds.Spec.ShareToNamespaceSelector != nil && !emptySelector(ds.Spec.ShareToNamespaceSelector) {
		if _, err := metav1.LabelSelectorAsSelector(ds.Spec.ShareToNamespaceSelector); err != nil {
			return fmt.Errorf("shareToNamespaceSelector is invalid: %w", err)
		}
	}
	policy := ds.Spec.ShareAccess
	if policy == nil {
		return nil
	}
	if len(policy.Rules) == 0 {
		return fmt.Errorf("shareAccess.rules must contain at least one rule when shareAccess is configured")
	}
	for i, rule := range policy.Rules {
		if rule.AccessMode != datasetv1alpha1.AccessModeReadOnly && rule.AccessMode != datasetv1alpha1.AccessModeReadWrite {
			return fmt.Errorf("shareAccess.rules[%d].accessMode %q is invalid", i, rule.AccessMode)
		}
		if emptySelector(&rule.NamespaceSelector) {
			return fmt.Errorf("shareAccess.rules[%d].namespaceSelector must not be empty", i)
		}
		if _, err := metav1.LabelSelectorAsSelector(&rule.NamespaceSelector); err != nil {
			return fmt.Errorf("shareAccess.rules[%d].namespaceSelector is invalid: %w", i, err)
		}
	}
	return nil
}

// Grant computes the access that source grants to targetNamespace. Namespace
// labels are always loaded from the API reader; callers never supply a
// workspace ID or a label map, which prevents a request from forging identity.
func Grant(ctx context.Context, reader client.Reader, source *datasetv1alpha1.Dataset, targetNamespace string) (GrantResult, error) {
	if source == nil {
		return Denied, fmt.Errorf("source dataset is required")
	}
	if targetNamespace == "" {
		return Denied, fmt.Errorf("target namespace is required")
	}
	if source.DeletionTimestamp != nil || !source.Spec.Share {
		return Denied, nil
	}
	if err := Validate(source); err != nil {
		return Denied, err
	}

	ns := &corev1.Namespace{}
	if err := reader.Get(ctx, client.ObjectKey{Name: targetNamespace}, ns); err != nil {
		return Denied, fmt.Errorf("get target namespace %q: %w", targetNamespace, err)
	}
	if source.Spec.ShareToNamespaceSelector != nil && !emptySelector(source.Spec.ShareToNamespaceSelector) {
		selector, err := metav1.LabelSelectorAsSelector(source.Spec.ShareToNamespaceSelector)
		if err != nil {
			return Denied, fmt.Errorf("parse shareToNamespaceSelector: %w", err)
		}
		if !selector.Matches(labels.Set(ns.Labels)) {
			return Denied, nil
		}
	}

	// A nil policy is the legacy sharing contract: shared references are
	// readable, never writable. An explicit policy has no fallback rule.
	if source.Spec.ShareAccess == nil {
		return ReadOnly, nil
	}

	grant := Denied
	for i, rule := range source.Spec.ShareAccess.Rules {
		selector, err := metav1.LabelSelectorAsSelector(&rule.NamespaceSelector)
		if err != nil {
			return Denied, fmt.Errorf("parse shareAccess.rules[%d].namespaceSelector: %w", i, err)
		}
		if !selector.Matches(labels.Set(ns.Labels)) {
			continue
		}
		// ReadOnly wins over every ReadWrite match.
		if rule.AccessMode == datasetv1alpha1.AccessModeReadOnly {
			return ReadOnly, nil
		}
		grant = ReadWrite
	}
	return grant, nil
}

// Resolve re-resolves every reference edge against the final Dataset namespace.
// It does not use upstream status.ReadOnly: status is a cached outcome and
// cannot grant access after a source policy changes.
func Resolve(ctx context.Context, reader client.Reader, ds *datasetv1alpha1.Dataset) (*Resolution, error) {
	if ds == nil || ds.Spec.Source.Type != datasetv1alpha1.DatasetTypeReference {
		return nil, fmt.Errorf("a REFERENCE dataset is required")
	}
	if err := Validate(ds); err != nil {
		return nil, err
	}

	seen := map[string]struct{}{datasetKey(ds.Namespace, ds.Name): {}}
	current := ds
	result := &Resolution{}
	for depth := 0; ; depth++ {
		if depth >= MaxReferenceDepth {
			return nil, fmt.Errorf("reference chain exceeds maximum depth %d", MaxReferenceDepth)
		}
		ref, err := ParseReference(current.Spec.Source.URI)
		if err != nil {
			return nil, err
		}
		key := datasetKey(ref.Namespace, ref.Name)
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("reference cycle detected at %s/%s", ref.Namespace, ref.Name)
		}
		seen[key] = struct{}{}

		source := &datasetv1alpha1.Dataset{}
		if err := reader.Get(ctx, ref, source); err != nil {
			return nil, fmt.Errorf("get source dataset %s/%s: %w", ref.Namespace, ref.Name, err)
		}
		if source.DeletionTimestamp != nil {
			return nil, fmt.Errorf("source dataset %s/%s is deleting", source.Namespace, source.Name)
		}
		grant, err := Grant(ctx, reader, source, ds.Namespace)
		if err != nil {
			return nil, err
		}
		if grant == Denied {
			return nil, fmt.Errorf("source dataset %s/%s is not shared to namespace %s", source.Namespace, source.Name, ds.Namespace)
		}
		result.Sources = append(result.Sources, source)
		if grant == ReadOnly {
			result.ReadOnly = true
		}
		if source.Spec.Source.Type != datasetv1alpha1.DatasetTypeReference {
			return result, nil
		}
		if source.Spec.VolumeClaimRef != nil {
			return nil, fmt.Errorf("source REFERENCE dataset %s/%s sets volumeClaimRef", source.Namespace, source.Name)
		}
		current = source
	}
}

// Bindings returns the status representation of the supplied source chain and
// verifies that every Dataset has an actual PVC and PV. It intentionally reads
// live objects rather than trusting an old status record.
func Bindings(ctx context.Context, reader client.Reader, sources []*datasetv1alpha1.Dataset) ([]datasetv1alpha1.MountSource, error) {
	if len(sources) == 0 {
		return nil, fmt.Errorf("reference source chain is empty")
	}
	bindings := make([]datasetv1alpha1.MountSource, 0, len(sources))
	for _, source := range sources {
		if source == nil || source.Status.PVCName == "" {
			return nil, fmt.Errorf("source dataset has no pvc")
		}
		pvc := &corev1.PersistentVolumeClaim{}
		if err := reader.Get(ctx, client.ObjectKey{Namespace: source.Namespace, Name: source.Status.PVCName}, pvc); err != nil {
			return nil, fmt.Errorf("get source pvc %s/%s: %w", source.Namespace, source.Status.PVCName, err)
		}
		if pvc.Status.Phase != corev1.ClaimBound || pvc.Spec.VolumeName == "" {
			return nil, fmt.Errorf("source pvc %s/%s is not bound", pvc.Namespace, pvc.Name)
		}
		pv := &corev1.PersistentVolume{}
		if err := reader.Get(ctx, client.ObjectKey{Name: pvc.Spec.VolumeName}, pv); err != nil {
			return nil, fmt.Errorf("get source pv %s: %w", pvc.Spec.VolumeName, err)
		}
		if pv.Spec.ClaimRef == nil || pv.Spec.ClaimRef.Namespace != pvc.Namespace || pv.Spec.ClaimRef.Name != pvc.Name || pv.Spec.ClaimRef.UID != pvc.UID {
			return nil, fmt.Errorf("source pv %s is not bound to pvc %s/%s", pv.Name, pvc.Namespace, pvc.Name)
		}
		bindings = append(bindings, datasetv1alpha1.MountSource{
			Namespace: source.Namespace,
			Name:      source.Name,
			UID:       string(source.UID),
			PVCName:   pvc.Name,
			PVCUID:    string(pvc.UID),
			PVName:    pv.Name,
			PVUID:     string(pv.UID),
		})
	}
	return bindings, nil
}

// Verify confirms that a reference Dataset is still mountable. It is the only
// method a pod-producing consumer should use to decide whether ReadOnly=false
// permits a writable volume.
func Verify(ctx context.Context, reader client.Reader, ds *datasetv1alpha1.Dataset) (*Resolution, error) {
	if ds == nil || ds.Spec.Source.Type != datasetv1alpha1.DatasetTypeReference {
		return nil, fmt.Errorf("a REFERENCE dataset is required")
	}
	if !mountPolicyReady(ds) {
		return nil, fmt.Errorf("MountPolicy is not ready for current generation")
	}
	resolution, err := Resolve(ctx, reader, ds)
	if err != nil {
		return nil, err
	}
	bindings, err := Bindings(ctx, reader, resolution.Sources)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(ds.Status.MountSources, bindings) {
		return nil, fmt.Errorf("mount source bindings no longer match status")
	}
	if ds.Status.ReadOnly != resolution.ReadOnly {
		return nil, fmt.Errorf("mount access mode no longer matches source policy")
	}
	if err := VerifyPVCBinding(ctx, reader, ds, resolution.Sources[0], bindings[0]); err != nil {
		return nil, err
	}
	for i, source := range resolution.Sources[:len(resolution.Sources)-1] {
		if source.Spec.Source.Type != datasetv1alpha1.DatasetTypeReference {
			continue
		}
		if err := verifyReferencePV(ctx, reader, source, bindings[i], bindings[i+1]); err != nil {
			return nil, err
		}
	}
	return resolution, nil
}

// VerifyPVCBinding validates the current reference Dataset's own PVC/PV. The
// source Dataset/PV are passed explicitly so this is also usable by the
// controller before it writes the MountPolicy=True condition.
func VerifyPVCBinding(ctx context.Context, reader client.Reader, ds *datasetv1alpha1.Dataset, source *datasetv1alpha1.Dataset, sourceBinding datasetv1alpha1.MountSource) error {
	if ds == nil || source == nil || ds.Status.PVCName == "" {
		return fmt.Errorf("reference dataset or its pvc is missing")
	}
	pvc := &corev1.PersistentVolumeClaim{}
	if err := reader.Get(ctx, client.ObjectKey{Namespace: ds.Namespace, Name: ds.Status.PVCName}, pvc); err != nil {
		return fmt.Errorf("get reference pvc %s/%s: %w", ds.Namespace, ds.Status.PVCName, err)
	}
	if !ownedBy(pvc.OwnerReferences, ds) {
		return fmt.Errorf("reference pvc %s/%s is not owned by dataset", pvc.Namespace, pvc.Name)
	}
	if pvc.Status.Phase != corev1.ClaimBound || pvc.Spec.VolumeName == "" {
		return fmt.Errorf("reference pvc %s/%s is not bound", pvc.Namespace, pvc.Name)
	}
	pv := &corev1.PersistentVolume{}
	if err := reader.Get(ctx, client.ObjectKey{Name: pvc.Spec.VolumeName}, pv); err != nil {
		return fmt.Errorf("get reference pv %s: %w", pvc.Spec.VolumeName, err)
	}
	if !ownedBy(pv.OwnerReferences, ds) {
		return fmt.Errorf("reference pv %s is not owned by dataset", pv.Name)
	}
	if pv.Spec.ClaimRef == nil || pv.Spec.ClaimRef.Namespace != pvc.Namespace || pv.Spec.ClaimRef.Name != pvc.Name || pv.Spec.ClaimRef.UID != pvc.UID {
		return fmt.Errorf("reference pv %s is not bound to its pvc", pv.Name)
	}
	sourcePV := &corev1.PersistentVolume{}
	if err := reader.Get(ctx, client.ObjectKey{Name: sourceBinding.PVName}, sourcePV); err != nil {
		return fmt.Errorf("get bound source pv %s: %w", sourceBinding.PVName, err)
	}
	if string(sourcePV.UID) != sourceBinding.PVUID {
		return fmt.Errorf("bound source pv %s no longer matches binding", sourceBinding.PVName)
	}
	return verifyClonePV(pv, source, sourceBinding, sourcePV)
}

// ProtectedPVC reports whether a PVC is the controller-managed PVC of any
// REFERENCE Dataset. Callers must treat an error as a denial, preventing an
// ordinary PVC Dataset from becoming an alias that bypasses mount policy.
func ProtectedPVC(ctx context.Context, reader client.Reader, namespace, pvcName string) (bool, error) {
	if namespace == "" || pvcName == "" {
		return false, fmt.Errorf("namespace and pvc name are required")
	}
	list := &datasetv1alpha1.DatasetList{}
	if err := reader.List(ctx, list); err != nil {
		return false, fmt.Errorf("list datasets for protected pvc check: %w", err)
	}
	for i := range list.Items {
		ds := &list.Items[i]
		if ds.Namespace != namespace || ds.Spec.Source.Type != datasetv1alpha1.DatasetTypeReference {
			continue
		}
		if referencePVCName(ds) == pvcName {
			return true, nil
		}
	}
	return false, nil
}

func verifyReferencePV(ctx context.Context, reader client.Reader, ds *datasetv1alpha1.Dataset, binding, parent datasetv1alpha1.MountSource) error {
	pvc := &corev1.PersistentVolumeClaim{}
	if err := reader.Get(ctx, client.ObjectKey{Namespace: binding.Namespace, Name: binding.PVCName}, pvc); err != nil {
		return fmt.Errorf("get reference source pvc %s/%s: %w", binding.Namespace, binding.PVCName, err)
	}
	if string(pvc.UID) != binding.PVCUID || pvc.Spec.VolumeName != binding.PVName {
		return fmt.Errorf("reference source pvc %s/%s no longer matches binding", binding.Namespace, binding.PVCName)
	}
	pv := &corev1.PersistentVolume{}
	if err := reader.Get(ctx, client.ObjectKey{Name: binding.PVName}, pv); err != nil {
		return fmt.Errorf("get reference source pv %s: %w", binding.PVName, err)
	}
	if string(pv.UID) != binding.PVUID || !ownedBy(pv.OwnerReferences, ds) {
		return fmt.Errorf("reference source pv %s no longer matches binding", binding.PVName)
	}
	parentDS := &datasetv1alpha1.Dataset{ObjectMeta: metav1.ObjectMeta{Namespace: parent.Namespace, Name: parent.Name, UID: types.UID(parent.UID)}}
	parentPV := &corev1.PersistentVolume{}
	if err := reader.Get(ctx, client.ObjectKey{Name: parent.PVName}, parentPV); err != nil {
		return fmt.Errorf("get parent pv %s: %w", parent.PVName, err)
	}
	if string(parentPV.UID) != parent.PVUID {
		return fmt.Errorf("parent pv %s no longer matches binding", parent.PVName)
	}
	return verifyClonePV(pv, parentDS, parent, parentPV)
}

func verifyClonePV(pv *corev1.PersistentVolume, source *datasetv1alpha1.Dataset, sourceBinding datasetv1alpha1.MountSource, sourcePV *corev1.PersistentVolume) error {
	if pv.Annotations == nil ||
		pv.Annotations[SourceDatasetUIDAnnotation] != string(source.UID) ||
		pv.Annotations[SourcePVCUIDAnnotation] != sourceBinding.PVCUID ||
		pv.Annotations[SourcePVUIDAnnotation] != sourceBinding.PVUID {
		return fmt.Errorf("reference pv %s source identity does not match", pv.Name)
	}
	if sourcePV == nil || !reflect.DeepEqual(pv.Spec.PersistentVolumeSource, sourcePV.Spec.PersistentVolumeSource) {
		return fmt.Errorf("reference pv %s persistent volume source no longer matches", pv.Name)
	}
	return nil
}

func mountPolicyReady(ds *datasetv1alpha1.Dataset) bool {
	for _, condition := range ds.Status.Conditions {
		if condition.Type == MountPolicyCondition && condition.Status == metav1.ConditionTrue && condition.ObservedGeneration == ds.Generation {
			return true
		}
	}
	return false
}

func emptySelector(selector *metav1.LabelSelector) bool {
	return selector == nil || (len(selector.MatchLabels) == 0 && len(selector.MatchExpressions) == 0)
}

// ParseReference validates a REFERENCE URI and returns its source Dataset key.
// It is shared by the controller and policy resolver so both paths accept the
// same URI grammar.
func ParseReference(uri string) (client.ObjectKey, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return client.ObjectKey{}, fmt.Errorf("parse reference uri %q: %w", uri, err)
	}
	name := strings.Trim(u.Path, "/")
	if u.Scheme != "dataset" || u.Host == "" || name == "" || strings.Contains(name, "/") || u.RawQuery != "" || u.Fragment != "" {
		return client.ObjectKey{}, fmt.Errorf("invalid reference uri %q, expected dataset://<namespace>/<dataset>", uri)
	}
	return client.ObjectKey{Namespace: u.Host, Name: name}, nil
}

func datasetKey(namespace, name string) string { return namespace + "/" + name }

func referencePVCName(ds *datasetv1alpha1.Dataset) string {
	if ds.Status.PVCName != "" {
		return ds.Status.PVCName
	}
	if ds.Spec.VolumeClaimTemplate.Name != "" {
		return ds.Spec.VolumeClaimTemplate.Name
	}
	return ds.Name
}

func ownedBy(refs []metav1.OwnerReference, ds *datasetv1alpha1.Dataset) bool {
	for _, ref := range refs {
		if ref.APIVersion == datasetv1alpha1.GroupVersion.String() && ref.Kind == "Dataset" && ref.Name == ds.Name && ref.UID == ds.UID {
			return true
		}
	}
	return false
}
