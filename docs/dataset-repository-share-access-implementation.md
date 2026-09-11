# Dataset Repository: Target-Workspace Share-Access Implementation Checklist

This document covers only the changes required in the `D:\github_repositories\dataset` repository (github.com/BaizeAI/dataset). It is released alongside baize's management API and Pod-mount consumers; it does not cover baize protobufs, UI, or training, Notebook, and inference code.

## 1. Goals and Boundaries

When Dataset A enables sharing, access is granted according to the workspace bound to the **target namespace in which the referencer resides**:

- Targets such as workspaces 1 and 3 receive read-only access.
- Targets such as workspaces 2 and 4 receive read-write access.
- Targets that do not match are denied when creating or using a reference.
- A's own direct use does not automatically become read-only merely because A is shared externally as read-only.

When `REFERENCE` Dataset B references A, it does not copy file data. The Controller prepares separate PVC/PV objects for B, while their underlying storage continues to point to A. B's `status.readOnly` is B's effective permission in its target namespace.

Do not add a global `spec.readOnly`. `ReadWriteMany` on the underlying PV/PVC represents the storage's concurrent-mount capability; it is not a business-level read/write permission assigned by namespace.

## 2. API and CRD

Modify `api/dataset/v1alpha1/dataset_types.go`, then regenerate `zz_generated.deepcopy.go` and the CRD.

### 2.1 `spec.shareAccess`

Add the following optional **pointer** field to `DatasetSpec`. The presence or absence of the field must remain semantically meaningful:

```go
ShareAccess *ShareAccess `json:"shareAccess,omitempty"`
```

Its value types are:

```go
type ShareAccess struct {
    Rules []ShareAccessRule `json:"rules"`
}

type ShareAccessRule struct {
    NamespaceSelector metav1.LabelSelector `json:"namespaceSelector"`
    // +kubebuilder:validation:Enum=ReadOnly;ReadWrite
    AccessMode AccessMode `json:"accessMode"`
}

// AccessMode permits only read-only or read-write. The kubebuilder marker enforces
// the enum at the CRD schema layer and rejects invalid values.
type AccessMode string

const (
    AccessModeReadOnly  AccessMode = "ReadOnly"
    AccessModeReadWrite AccessMode = "ReadWrite"
)
```

Constraints:

- `accessMode` may only be `ReadOnly` or `ReadWrite`; enforce this at the CRD schema layer with the kubebuilder Enum marker on `AccessMode`.
- `namespaceSelector` is required and must not be an empty selector.
- When `share=true` and `shareAccess` is present, `rules` must contain at least one rule.
- `shareAccess` is immutable after creation: it cannot be changed, deleted, or populated later on historical objects.
- When `share=false`, a non-empty `shareAccess` may be preconfigured at creation time. Preserve it and grant access according to its rules when sharing is later enabled. If it is absent at creation, it cannot be populated later; after sharing is enabled, continue to use the historical read-only model.
- `shareAccess` being absent must be distinguished from an explicitly empty object or empty `rules`. Only absence selects the backward-compatible model; it does not mean read-write. Historical shared Datasets are always read-only within the range of the original `shareToNamespaceSelector`. An explicit empty configuration must be rejected when `share=true`; it must never fall back to the historical model.
- `shareToNamespaceSelector` remains the overall sharing boundary. Actual authorization must pass both it and `shareAccess.rules`.

Use kubebuilder validation/CEL at the CRD layer to validate field presence and immutability. Immutability rules must be based on `has(self.shareAccess)` / `has(oldSelf.shareAccess)`; do not collapse the pointer field into a value type. The Controller must also defensively reject invalid CRs submitted directly.

### 2.2 `status.mountSources`

Add the following field to `DatasetStatus`. For a `REFERENCE` Dataset, it is the complete reference chain ordered from its direct source to its ultimate root source, and **does not include itself**: for `C -> B -> A`, `C.status.mountSources` is `[B, A]`. Each item records the corresponding Dataset and its actual PVC/PV identities. C's own PVC is found via `C.status.PVCName`, and its actual UID, owner, and PV binding are verified in `Bindings`/`Verify`. This field is not written for non-`REFERENCE` Datasets.

```go
MountSources []MountSource `json:"mountSources,omitempty"`
```

The item type binds the authorization policy to the actual storage:

```go
type MountSource struct {
    Namespace string `json:"namespace"`
    Name      string `json:"name"`
    UID       string `json:"uid"`
    PVCName   string `json:"pvcName"`
    PVCUID    string `json:"pvcUID"`
    PVName    string `json:"pvName"`
    PVUID     string `json:"pvUID"`
}
```

Keep `status.readOnly`; it represents the current Dataset's effective mount permission, not A's global policy for all namespaces. When a reference Dataset succeeds, write `status.readOnly`, the complete `status.mountSources`, and a `MountPolicy=True` condition carrying the current Dataset's `observedGeneration` in the same status update. On failure, write `MountPolicy=False`; do not allow an old `readOnly=false` to continue being treated as authorization.

Keep `ReadOnly` as `omitempty`: when serialized to JSON, `readOnly=false` is omitted, so unstructured/JSON consumers cannot distinguish “write allowed” from “not yet computed.” `MountPolicy=True` with an `observedGeneration` equal to the current generation is therefore the authoritative signal that the permission has been computed successfully. A missing or stale condition must never permit writes.

## 3. Shared Authorization Resolver

Add `pkg/mountpolicy`, shared by the Controller and baize, so the two sides do not interpret the rules independently.

It should provide at least the following capabilities:

| Function | Responsibility |
| --- | --- |
| `Validate` | Validate rule modes, selectors, and sharing configuration |
| `Grant` | Determine Denied / ReadOnly / ReadWrite for a source Dataset and target namespace |
| `Resolve` | Resolve the reference chain, cycles, and depth, and calculate the effective read-only result |
| `Bindings` | Read and verify the UIDs of the Dataset, PVC, and PV, along with the actual PV source |
| `Verify` | Let consumers confirm that a reference Dataset is prepared; that `MountPolicy=True`; that the condition's `observedGeneration` equals the current generation; that the full source chain and PVC/PV identities still match; and that resolving the current chain again still grants access |
| `ProtectedPVC` | Determine whether a PVC is the protected volume of a reference Dataset, preventing alias-based bypasses |

### 3.1 Single-Level Rules

`Grant(source, targetNamespace)` follows these rules:

1. Deny if the source is not shared or is being deleted.
2. If `shareToNamespaceSelector` is nil or structurally empty, preserve the existing semantics: the overall sharing range is every namespace. Deny only when the selector is non-empty and the target does not match.
3. If `shareAccess` is absent, return read-only within the overall range for backward compatibility. Thus, a historical shared Dataset whose overall range is every namespace is read-only in every namespace.
4. If no new rule matches, deny. A nil or empty `shareToNamespaceSelector` does not relax `shareAccess.rules`; new rules still authorize only matching targets.
5. When multiple rules match, read-only takes precedence; return read-write only if all matching rules are read-write.
6. Fail closed for any selector, namespace, or object-read error; never fall back to read-write.

Workspace labels on the target namespace must originate from a trusted platform identity. Do not trust a workspace ID supplied by the caller.

Before generating a Pod, consumers must call `Verify`, rather than decide writability from `status.readOnly` alone. `Verify` must fail closed for any missing, stale, identity-mismatched, chain-re-resolution-failed, or unauthorized result. Only after successful verification may a consumer interpret `status.readOnly=false` as write permission.

### 3.2 Multi-Level References

Support `C -> B -> A`. The recommended maximum depth is 32; reject self-references, cycles, deleted sources, missing sources, and `REFERENCE + volumeClaimRef`.

For C's final namespace:

- Every direct reference edge must be authorized.
- Every upstream A/B must also authorize the final namespace.
- If any edge is read-only, the final result is read-only.
- If any edge is denied, reject the entire reference.

Do not treat an upstream legacy `status.readOnly=false` as authorization; resolve the current chain again.

## 4. Dataset Controller

Modify `internal/controller/dataset/dataset_controller.go`.

### 4.1 Validation and Reconciliation Order

For `REFERENCE`:

```text
Validate sharing rules and the reference chain
-> Prepare/confirm B's PVC and PV
-> Calculate MountPolicy, status.readOnly, and MountSources
-> Write the status condition
```

Requirements:

- Remove the existing logic that unconditionally writes `status.readOnly=true`.
- Even when the PVC is Ready, continue MountPolicy reconciliation; do not return early.
- If existing `MountSources` do not match the currently resolved sources, deny authorization; do not rebind to a newly created A with the same name.
- Set `MountPolicy=False` when MountPolicy fails. A PVC-preparation failure must also set `MountPolicy=False` (or calculate authorization before PVC preparation); no previous `MountPolicy=True` may remain. A condition's `observedGeneration` reflects only this object's spec generation, so upstream policy changes do not make it stale; do not rely on chain re-resolution in `Verify` as an implicit safeguard.
- A `REFERENCE` Dataset must keep retrying periodically after entering Failed: change the `Reconcile()` branch that currently returns `resOk` immediately for `Failed` so that a REFERENCE returns `res30sec` (or an equivalent bounded `RequeueAfter`). An error from any reconciler interrupts later steps, so recovery cannot rely solely on watch events.
- Watch Dataset and Namespace changes and requeue every direct and transitive reference. Periodic reconciliation is a fallback for lost watches and source recovery.
- An authorization-resolution failure must not block deletion through the finalizer path.

### 4.2 PVC/PV Lifecycle

B's reference PV is a copy of A's PV configuration. At creation it must:

- Clear `ResourceVersion`, UID, creation timestamp, managed fields, status, and `claimRef`.
- Record the source Dataset UID and source PV UID.
- Use B as the owner of B's PVC/PV.
- Fix the cloned PV's `persistentVolumeReclaimPolicy` to `Retain`, preventing deletion of B from deleting the shared source data.
- Verify B's actual PVC/PV UIDs, PV `claimRef`, and underlying `PersistentVolumeSource` match the recorded values.
- Avoid a fixed-length UID slice when naming cloned PVs; use a safe truncation helper or the full UID, so short fake-client UIDs cannot panic.

Deleting B may reclaim B's objects but must not delete A's underlying data. Do not recreate A's storage in response to a permission change.

### 4.3 Preventing PVC Alias Bypasses

Reject an ordinary `PVC` Dataset or `volumeClaimRef` when it points to the PVC of reference Dataset B, and instruct users to use the original Dataset/Model reference path. `ProtectedPVC` must use the live Dataset list; do not allow access when the query fails or associations conflict.

## 5. Tests

Add at least the following tests:

- Read-only groups, read-write groups, unmatched namespaces, and historical objects with `shareAccess` absent.
- A nil or structurally empty `shareToNamespaceSelector`: historical objects are read-only in all namespaces; objects using new rules still authorize only namespaces matching `shareAccess.rules`.
- Valid `shareAccess` preconfigured when created with `share=false`, then authorization according to the original rules after sharing is enabled; also distinguish an absent field, an explicitly empty object, and empty `rules`.
- When JSON/unstructured representation omits `status.ReadOnly=false`, `Verify` may return writable only with `MountPolicy=True` and matching `observedGeneration`.
- Read-only wins where overlapping rules match the same namespace.
- Multi-level references: upstream read-only, upstream not authorizing the final namespace, fully read-write chains, cycles, and excessive depth.
- Effective permission is reconciled again after a Ready PVC when namespace workspace labels or source policy changes.
- A REFERENCE that enters Failed due to a temporary source/policy error automatically recovers through periodic requeue even without an additional watch event.
- A PVC-preparation failure (such as a source without a PVC or a PVC not bound to a PV) must set `MountPolicy=False` and must not leave an old condition behind.
- Deny mounting when the source Dataset/PVC/PV is recreated or its UID does not match.
- For `C -> B -> A`, `C.status.mountSources` must be exactly `[B, A]` and exclude C. Validate C's own volume with `status.PVCName` and actual UID/owner/PV binding. `Verify` must reject a same-name recreation of any chain member or a change to PVC/PV UIDs or the actual PV source.
- Reject wrapping B's PVC in an ordinary PVC Dataset or `volumeClaimRef`.
- The cloned PV retains `Retain` and source UIDs; short fake-client UIDs do not panic during PV-name slicing.
- CRD schema/CEL: invalid modes, empty selectors, empty rules, and creation-time modification, deletion, or later population of `shareAccess`.

The fake client may cover Controller branches; CRD/CEL should also use the Kubernetes API validator or envtest. Before the final release, validate in a real NFS/CSI environment: read-only workspaces cannot write, read-write workspaces can write, and deleting B does not affect A's data.

## 6. Release Contract with baize

baize must depend on the module version published from this repository rather than rely indefinitely on a local `go.work` reference to `../dataset`.

Release order:

1. Release the Dataset API, CRD, and Controller; first complete MountPolicy/source-identity reconciliation for historical reference objects.
2. Release baize's management API and mount consumers. Every Pod-generation entry point must use the same `Verify` result before generating volumes and `volumeMounts`; it must not read only `status.readOnly`.
3. After validating version compatibility, historical template migration, and real storage, enable configuration of read-write workspaces.

Do not open the new read-write rules until the Controller and CRD are fully upgraded, so an old Controller cannot hard-code every reference as read-only.

Rollback boundary: once read-write rules and running read-write Pods exist, a lossless rollback is impossible—the old Controller will change references back to read-only, but mounts of already running Pods remain unchanged. For rollback, first disable the read-write workspace configuration entry point, then prioritize a forward fix and reconcile workload recreation. Do not roll back by deleting `shareAccess` or recreating the underlying storage.
