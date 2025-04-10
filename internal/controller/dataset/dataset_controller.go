/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package dataset

import (
	"context"
	"fmt"
	"github.com/BaizeAI/dataset/config"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/labels"

	"github.com/BaizeAI/dataset/pkg/kubeutils"

	"github.com/samber/lo"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/yaml"

	"github.com/BaizeAI/dataset/internal/pkg/constants"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/BaizeAI/dataset/pkg/log"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	datasetv1alpha1 "github.com/BaizeAI/dataset/api/dataset/v1alpha1"
)

const (
	datasetFinalizer = "dataset-controller"
	keepConditions   = 5

	condTypeConfig = "Config"
)

// DatasetReconciler reconciles a Dataset object
type DatasetReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

type reconciler struct {
	typ string
	rec func(ctx context.Context, ds *datasetv1alpha1.Dataset) error
}

//+kubebuilder:rbac:groups=dataset.baizeai.io,resources=datasets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=dataset.baizeai.io,resources=datasets/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=dataset.baizeai.io,resources=datasets/finalizers,verbs=update

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
func (r *DatasetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ds := &datasetv1alpha1.Dataset{}
	err := r.Get(ctx, req.NamespacedName, ds)
	if err != nil {
		log.Errorf("error fetch dataset for %v: error: %v", req, err)
		return ctrl.Result{}, nil
	}

	status := ds.Status.DeepCopy()
	var reconcilers []reconciler
	if kubeutils.IsDeleted(ds) {
		reconcilers = []reconciler{
			// {typ: "Job", rec: r.reconcileJob},  // 同样可以加上清理 job 的逻辑
			{typ: "PVC", rec: r.reconcilePVC},
			{typ: "", rec: r.reconcileFinalizer},
		}
	} else {
		reconcilers = []reconciler{
			{typ: condTypeConfig, rec: r.validate},
			{typ: "", rec: r.reconcileFinalizer},
			{typ: "PVC", rec: r.reconcilePVC},
			{typ: "ConfigMap", rec: r.reconcileConfigMap},
			{typ: "Job", rec: r.reconcileJob},
			{typ: "JobStatus", rec: r.reconcileJobStatus},
		}
	}

	for _, rr := range reconcilers {
		log.Debugf("start reconciling dataset for %s/%s: %+v...", ds.Namespace, ds.Name, rr)
		err := rr.rec(ctx, ds)
		ds.Status.Conditions = kubeutils.SetCondition(ds.Status.Conditions, rr.typ, err)
		if err != nil {
			log.Errorf("error reconciling dataset for %s/%s: %v", ds.Namespace, ds.Name, err)
			break
		}
	}

	_ = r.reconcilePhase(ctx, ds)
	res30sec := ctrl.Result{
		RequeueAfter: time.Second * 30,
	}
	res5sec := ctrl.Result{
		RequeueAfter: time.Second * 5,
	}
	resOk := ctrl.Result{}

	if !reflect.DeepEqual(ds.Status, *status) {
		err := r.Status().Update(ctx, ds)
		if err != nil {
			log.Errorf("error update status for %s/%s: %v", ds.Namespace, ds.Name, err)
			return res30sec, err
		}
	}

	switch ds.Status.Phase {
	case datasetv1alpha1.DatasetStatusPhaseReady, datasetv1alpha1.DatasetStatusPhaseFailed:
		return resOk, nil
	case datasetv1alpha1.DatasetStatusPhaseProcessing:
		return res5sec, nil
	default:
		return res30sec, nil
	}
}

func supportPreload(ds *datasetv1alpha1.Dataset) bool {
	switch ds.Spec.Source.Type {
	case datasetv1alpha1.DatasetTypeGit,
		datasetv1alpha1.DatasetTypeS3,
		datasetv1alpha1.DatasetTypeHTTP,
		datasetv1alpha1.DatasetTypeConda,
		datasetv1alpha1.DatasetTypeHuggingFace,
		datasetv1alpha1.DatasetTypeModelScope:
		return true
	default:
		return false
	}
}

func genJobName(dsName string, round int32) string {
	return fmt.Sprintf("dataset-%s-round-%d", dsName, round)
}

func datasetOwnerRef(ds *datasetv1alpha1.Dataset) []metav1.OwnerReference {
	return []metav1.OwnerReference{*metav1.NewControllerRef(ds, datasetv1alpha1.GroupVersion.WithKind("Dataset"))}
}

func forceDelete(ds *datasetv1alpha1.Dataset) bool {
	return ds.DeletionTimestamp != nil && ds.DeletionTimestamp.Add(time.Minute*5).Before(time.Now())
}

func (r *DatasetReconciler) reconcileFinalizer(ctx context.Context, ds *datasetv1alpha1.Dataset) error {
	if kubeutils.IsDeleted(ds) {
		ds.Finalizers = nil
		return r.Update(ctx, ds)
	}
	if lo.Contains(ds.Finalizers, datasetFinalizer) {
		return nil
	}
	ds.Finalizers = []string{datasetFinalizer}
	return r.Update(ctx, ds)
}

func (r *DatasetReconciler) reconcilePVC(ctx context.Context, ds *datasetv1alpha1.Dataset) error {
	pvcName := ds.Name
	if v := ds.Spec.VolumeClaimTemplate.Name; v != "" {
		pvcName = v
	}

	forceStorageClass := ""
	var spec *corev1.PersistentVolumeClaimSpec
	volumeNameOverride := ""

	switch ds.Spec.Source.Type {
	case datasetv1alpha1.DatasetTypeReference:
		if kubeutils.IsDeleted(ds) {
			// OwnerReference 会将其自动回收，这里不做额外 Delete
			return nil
		}
		srcDs, err := r.getSourceDataset(ctx, ds)
		if err != nil {
			return err
		}
		if srcDs.Status.PVCName == "" {
			return fmt.Errorf("source dataset %s/%s has no pvc", srcDs.Namespace, srcDs.Name)
		}
		// 先获取 source dataset 的 pvc
		pvc := &corev1.PersistentVolumeClaim{}
		err = r.Get(ctx, client.ObjectKey{Namespace: srcDs.Namespace, Name: srcDs.Status.PVCName}, pvc)
		if err != nil {
			return fmt.Errorf("get pvc %s/%s for source dataset %s/%s error: %v",
				srcDs.Namespace, srcDs.Status.PVCName,
				srcDs.Namespace, srcDs.Name, err)
		}
		if pvc.Spec.VolumeName == "" {
			return fmt.Errorf("pvc %s/%s has no volume", pvc.Namespace, pvc.Name)
		}
		// 再获取 source dataset pvc 对应的 pv
		pv := &corev1.PersistentVolume{}
		err = r.Get(ctx, client.ObjectKey{Name: pvc.Spec.VolumeName}, pv)
		if err != nil {
			return fmt.Errorf("get pv %s for source dataset %s/%s error: %v",
				pvc.Spec.VolumeName, srcDs.Namespace, srcDs.Name, err)
		}
		// 克隆一个新的 pv 给当前 ds
		newPv := pv.DeepCopy()
		newPv.OwnerReferences = datasetOwnerRef(ds)
		newPv.Name = fmt.Sprintf("dataset-%s-pvc-%s", ds.Namespace, ds.Name)
		if newPv.Labels == nil {
			newPv.Labels = make(map[string]string)
		}
		newPv.Labels[constants.DatasetNameLabel] = ds.Name
		newPv.ResourceVersion = ""
		newPv.Spec.ClaimRef = nil
		// 保留策略改为 Retain
		newPv.Spec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimRetain
		if err := r.Get(ctx, client.ObjectKey{Name: newPv.Name}, pv); err != nil {
			if !k8serrors.IsNotFound(err) {
				return err
			}
			if err := r.Create(ctx, newPv); err != nil {
				return err
			}
		}
		spec = pvc.Spec.DeepCopy()
		spec.VolumeName = newPv.Name

		// 标记当前 dataset 状态
		ds.Status.LastSucceedRound = ds.Spec.DataSyncRound
		ds.Status.ReadOnly = true

	case datasetv1alpha1.DatasetTypePVC:
		u, err := url.Parse(ds.Spec.Source.URI)
		if err != nil {
			return err
		}
		pvcName = u.Host

		// 如果已经删除，尝试把 PVC 上的 label 清空
		if kubeutils.IsDeleted(ds) {
			pvc := &corev1.PersistentVolumeClaim{}
			err = r.Get(ctx, client.ObjectKey{Namespace: ds.Namespace, Name: pvcName}, pvc)
			if err == nil {
				if dsName, exists := pvc.Labels[constants.DatasetNameLabel]; exists && dsName == ds.Name {
					delete(pvc.Labels, constants.DatasetNameLabel)
					if updateErr := r.Update(ctx, pvc); updateErr != nil {
						log.Errorf("update pvc %s/%s for deletion %s error: %v",
							ds.Namespace, pvcName, ds.Name, updateErr)
					}
				}
			}
			return nil
		}

		// PVC 存在与否都走一下
		pvc := &corev1.PersistentVolumeClaim{}
		err = r.Get(ctx, client.ObjectKey{Namespace: ds.Namespace, Name: pvcName}, pvc)
		if err != nil {
			return err
		}
		if dsName, exists := pvc.Labels[constants.DatasetNameLabel]; exists && dsName != ds.Name {
			return fmt.Errorf("pvc %s is not belong to dataset %s/%s", pvcName, ds.Namespace, ds.Name)
		} else if !exists {
			if pvc.Labels == nil {
				pvc.Labels = make(map[string]string)
			}
			pvc.Labels[constants.DatasetNameLabel] = ds.Name
			if err = r.Update(ctx, pvc); err != nil {
				return err
			}
		}
		ds.Status.PVCName = pvcName
		return nil

	case datasetv1alpha1.DatasetTypeNFS:
		pvName := fmt.Sprintf("dataset-%s-pvc-%s", ds.Namespace, pvcName)

		if kubeutils.IsDeleted(ds) {
			// 删除对应的 pv
			pv := &corev1.PersistentVolume{
				ObjectMeta: metav1.ObjectMeta{
					Name: pvName,
				},
			}
			if err := r.Delete(ctx, pv); err != nil && !k8serrors.IsNotFound(err) {
				if forceDelete(ds) {
					log.Errorf("delete pv %s for %s/%s error: %v, but force delete",
						pvName, ds.Namespace, ds.Name, err)
					return nil
				}
				return err
			}
			return nil
		}

		// NFS 需要先创建一个 PV
		var pvTemp corev1.PersistentVolume
		err := yaml.Unmarshal([]byte(`
apiVersion: v1
kind: PersistentVolume
metadata:
  annotations:
    pv.kubernetes.io/provisioned-by: nfs.csi.k8s.io
spec:
  capacity:
    storage: 100Ti
  accessModes:
    - ReadWriteMany
  persistentVolumeReclaimPolicy: Retain
  storageClassName: nfs-csi
  mountOptions:
    - nfsvers=4.1
  csi:
    driver: nfs.csi.k8s.io
`), &pvTemp)
		if err != nil {
			return err
		}
		u, err := url.Parse(ds.Spec.Source.URI)
		if err != nil {
			return err
		}
		forceStorageClass = pvTemp.Spec.StorageClassName

		pv := &corev1.PersistentVolume{}
		getErr := r.Get(ctx, client.ObjectKey{Name: pvName}, pv)
		if getErr != nil && !k8serrors.IsNotFound(getErr) {
			return getErr
		} else if getErr == nil {
			if pv.Labels[constants.DatasetNameLabel] != ds.Name {
				return fmt.Errorf("pv %s is not belong to dataset %s/%s", pvName, ds.Namespace, ds.Name)
			}
		} else {
			// 需要新建
			pvTemp.OwnerReferences = datasetOwnerRef(ds)
			if pvTemp.Labels == nil {
				pvTemp.Labels = make(map[string]string)
			}
			pvTemp.Labels[constants.DatasetNameLabel] = ds.Name
			pvTemp.Name = pvName

			if pvTemp.Spec.CSI == nil {
				pvTemp.Spec.CSI = &corev1.CSIPersistentVolumeSource{}
			}
			if pvTemp.Spec.CSI.VolumeAttributes == nil {
				pvTemp.Spec.CSI.VolumeAttributes = make(map[string]string)
			}
			pvTemp.Spec.CSI.VolumeAttributes["server"] = u.Host
			pvTemp.Spec.CSI.VolumeAttributes["share"] = "/"
			pvTemp.Spec.CSI.VolumeAttributes["subdir"] = u.Path
			pvTemp.Spec.CSI.VolumeAttributes["onDelete"] = "retain"
			pvTemp.Spec.CSI.VolumeAttributes["csi.storage.k8s.io/pv/name"] = pvName
			pvTemp.Spec.CSI.VolumeAttributes["csi.storage.k8s.io/pvc/name"] = pvcName
			pvTemp.Spec.CSI.VolumeAttributes["csi.storage.k8s.io/pvc/namespace"] = ds.Namespace

			// 如果 mountPermissions 没配置，则默认用 ds.Spec.MountOptions.Mode
			if pvTemp.Spec.CSI.VolumeAttributes["mountPermissions"] == "" {
				pvTemp.Spec.CSI.VolumeAttributes["mountPermissions"] = ds.Spec.MountOptions.Mode
			}
			pvTemp.Spec.CSI.VolumeHandle = fmt.Sprintf("%s#%s#%s#", u.Host, u.Path, pvName)

			if err := r.Create(ctx, &pvTemp); err != nil {
				return err
			}
		}
		// 标记 ds.Status.LastSucceedRound = ds.Spec.DataSyncRound
		ds.Status.LastSucceedRound = ds.Spec.DataSyncRound
		volumeNameOverride = pvTemp.Name
	default:
		// 其他类型先不做特殊逻辑
	}

	// 如果 ds 已经是删除态，则删除对应的 PVC
	if kubeutils.IsDeleted(ds) {
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pvcName,
				Namespace: ds.Namespace,
			},
		}
		if err := r.Delete(ctx, pvc); err != nil && !k8serrors.IsNotFound(err) {
			if forceDelete(ds) {
				log.Errorf("delete pvc %s/%s for dataset %s error: %v, but force delete",
					ds.Namespace, pvcName, ds.Name, err)
				return nil
			}
			return err
		}
		return nil
	}

	ds.Status.PVCName = pvcName

	// 除了 reference 类型外，其他都需要按模板创建 PVC
	pvc := &corev1.PersistentVolumeClaim{}
	err := r.Get(ctx, client.ObjectKey{Namespace: ds.Namespace, Name: pvcName}, pvc)
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	if spec == nil { // 普通模板
		spec = ds.Spec.VolumeClaimTemplate.Spec.DeepCopy()
		if len(spec.AccessModes) == 0 {
			spec.AccessModes = []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteMany,
			}
		}
		if spec.VolumeMode == nil {
			vm := corev1.PersistentVolumeFilesystem
			spec.VolumeMode = &vm
		}
		if spec.Resources.Requests == nil {
			spec.Resources.Requests = corev1.ResourceList{}
		}
		quantity := spec.Resources.Requests[corev1.ResourceStorage]
		if quantity.IsZero() {
			spec.Resources.Requests[corev1.ResourceStorage] = resource.MustParse("100Ti")
		}
		if forceStorageClass != "" {
			// nfs 强制使用 nfs storageclass
			spec.StorageClassName = lo.ToPtr(forceStorageClass)
		}
	}
	if volumeNameOverride != "" {
		spec.VolumeName = volumeNameOverride
	}

	if k8serrors.IsNotFound(err) {
		// 不存在就创建
		newPVC := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pvcName,
				Namespace: ds.Namespace,
				Labels: lo.Assign(ds.Labels, map[string]string{
					constants.DatasetNameLabel: ds.Name,
				}),
				Annotations:     ds.Annotations,
				OwnerReferences: datasetOwnerRef(ds),
			},
			Spec: *spec,
		}
		if err = r.Create(ctx, newPVC); err != nil {
			return err
		}
	} else {
		if pvc.Labels[constants.DatasetNameLabel] != ds.Name {
			return fmt.Errorf("pvc %s already exists, but not belong to dataset %s", pvcName, ds.Name)
		}
	}

	return nil
}

func (r *DatasetReconciler) reconcileConfigMap(ctx context.Context, ds *datasetv1alpha1.Dataset) error {
	if ds.Spec.Source.Type != datasetv1alpha1.DatasetTypeConda {
		return nil
	}

	existingCm, err := r.getConfigMap(ctx, ds)
	if err != nil {
		return err
	}

	configMapOptions := make([]condaOption, 0, 2)
	if yamlData, ok := ds.Spec.Source.Options["condaEnvironmentYml"]; ok && strings.TrimSpace(yamlData) != "" {
		configMapOptions = append(configMapOptions, withCondaEnvironmentYAML(yamlData))
	}
	if txt, ok := ds.Spec.Source.Options["pipRequirementsTxt"]; ok && strings.TrimSpace(txt) != "" {
		configMapOptions = append(configMapOptions, withPipRequirementsTxt(txt))
	}

	if existingCm == nil {
		_, err := r.createConfigMap(ctx, ds, configMapOptions...)
		if err != nil {
			return err
		}
		return nil
	}

	// update existing configmap
	_, err = r.updateConfigMap(ctx, existingCm, configMapOptions...)
	if err != nil {
		return err
	}

	return nil
}

func (r *DatasetReconciler) reconcileJob(ctx context.Context, ds *datasetv1alpha1.Dataset) error {
	if !supportPreload(ds) {
		return nil
	}
	if kubeutils.IsDeleted(ds) {
		// 原先用 DeleteCollection()，controller-runtime 可以使用 DeleteAllOf 或者先 List 然后循环 Delete
		jobList := &batchv1.JobList{}
		if err := r.List(ctx, jobList, client.InNamespace(ds.Namespace), client.MatchingLabels{
			constants.DatasetNameLabel: ds.Name,
		}); err != nil && !k8serrors.IsNotFound(err) {
			if forceDelete(ds) {
				log.Errorf("delete jobs for dataset %s/%s error: %v, but force delete", ds.Namespace, ds.Name, err)
				return nil
			}
			return err
		}
		for i := range jobList.Items {
			if err := r.Delete(ctx, &jobList.Items[i]); err != nil && !k8serrors.IsNotFound(err) {
				if forceDelete(ds) {
					log.Errorf("delete job %s/%s for dataset %s/%s error: %v, but force delete",
						jobList.Items[i].Namespace, jobList.Items[i].Name, ds.Namespace, ds.Name, err)
					return nil
				}
				return err
			}
		}
		return nil
	}

	// 若 dataSyncRound > lastSucceedRound，则需要创建新的 job
	if ds.Spec.DataSyncRound > ds.Status.LastSucceedRound {
		ds.Status.InProcessing = true
		ds.Status.InProcessingRound = ds.Spec.DataSyncRound
		jobName := genJobName(ds.Name, ds.Status.InProcessingRound)

		jobSpec := batchv1.JobSpec{}
		err := yaml.Unmarshal([]byte(config.GetDatasetJobSpecYaml()), &jobSpec)
		if err != nil {
			log.Errorf("unmarshal dataset job spec yaml failed: %v", err)
		}

		container := &jobSpec.Template.Spec.Containers[0]
		container.Name = "dataset-loader"

		// 预留资源请求
		containerRequests := make(corev1.ResourceList)
		containerLimits := make(corev1.ResourceList)

		switch ds.Spec.Source.Type {
		case datasetv1alpha1.DatasetTypeConda:
			containerRequests[corev1.ResourceCPU] = resource.MustParse("2")
			containerRequests[corev1.ResourceMemory] = resource.MustParse("2Gi")
			containerLimits[corev1.ResourceCPU] = resource.MustParse("4")
			containerLimits[corev1.ResourceMemory] = resource.MustParse("4Gi")

		case datasetv1alpha1.DatasetTypeHuggingFace,
			datasetv1alpha1.DatasetTypeModelScope:
			containerRequests[corev1.ResourceCPU] = resource.MustParse("2")
			containerRequests[corev1.ResourceMemory] = resource.MustParse("2Gi")
			containerLimits[corev1.ResourceCPU] = resource.MustParse("4")
			containerLimits[corev1.ResourceMemory] = resource.MustParse("8Gi")
		}

		// 如果有 GPU 需求
		if gpuType, ok := ds.Spec.Source.Options["gpuType"]; ok {
			switch gpuType {
			case "nvidia-gpu":
				containerRequests["nvidia.com/gpu"] = resource.MustParse("1")
				containerLimits["nvidia.com/gpu"] = resource.MustParse("1")
			case "nvidia-vgpu":
				containerRequests["nvidia.com/vgpu"] = resource.MustParse("1")
				containerRequests["nvidia.com/gpumem"] = resource.MustParse("500")
				containerLimits["nvidia.com/vgpu"] = resource.MustParse("1")
				containerLimits["nvidia.com/gpumem"] = resource.MustParse("500")
			case "metax-gpu":
				containerRequests["metax-tech.com/gpu"] = resource.MustParse("1")
				containerLimits["metax-tech.com/gpu"] = resource.MustParse("1")
			}
		}

		if len(containerRequests) > 0 {
			container.Resources.Requests = containerRequests
		}
		if len(containerLimits) > 0 {
			container.Resources.Limits = containerLimits
		}

		options := make(map[string]string)
		for k, v := range ds.Spec.Source.Options {
			options[k] = v
		}

		podSpec := &jobSpec.Template.Spec

		// conda 类型需要将 ConfigMap mount 到容器
		condaKeyItems := make([]corev1.KeyToPath, 0, 2)
		condaPodVolumeName := "dataset-config-conda"

		switch ds.Spec.Source.Type {
		case datasetv1alpha1.DatasetTypeConda:
			if yamlData, ok := options["condaEnvironmentYml"]; ok && strings.TrimSpace(yamlData) != "" {
				delete(options, "condaEnvironmentYml")
				condaKeyItems = append(condaKeyItems, corev1.KeyToPath{
					Key:  constants.DatasetJobCondaCondaEnvironmentYAMLFilename,
					Path: constants.DatasetJobCondaCondaEnvironmentYAMLFilename,
				})
			}
			if txt, ok := options["pipRequirementsTxt"]; ok && strings.TrimSpace(txt) != "" {
				delete(options, "pipRequirementsTxt")
				condaKeyItems = append(condaKeyItems, corev1.KeyToPath{
					Key:  constants.DatasetJobCondaPipRequirementsTxtFilename,
					Path: constants.DatasetJobCondaPipRequirementsTxtFilename,
				})
			}
		}

		if ds.Spec.Source.Type == datasetv1alpha1.DatasetTypeConda && len(condaKeyItems) > 0 {
			podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
				Name: condaPodVolumeName,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: datasetConfigMapName(ds),
						},
						Items: condaKeyItems,
					},
				},
			})
			container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
				Name:      condaPodVolumeName,
				MountPath: constants.DatasetJobCondaConfigDir,
				ReadOnly:  true,
			})
		}

		// 如果有 SecretRef
		if ds.Spec.SecretRef != "" {
			podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
				Name: "dataset-secret",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: ds.Spec.SecretRef,
					},
				},
			})
			container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
				Name:      "dataset-secret",
				MountPath: constants.DatasetJobSecretsMountPath,
				ReadOnly:  true,
			})
		}

		// 绑定 PVC
		pvcMountPath := "/baize/dataset/data"
		podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
			Name: "dataset-pvc",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: ds.Status.PVCName,
				},
			},
		})
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
			Name:      "dataset-pvc",
			MountPath: pvcMountPath,
		})

		// 构造命令行参数
		switch ds.Spec.Source.Type {
		case datasetv1alpha1.DatasetTypeConda:
			// 这里把 gpuType 拿掉，已经单独处理过
			delete(options, "gpuType")
		}

		args := []string{
			string(ds.Spec.Source.Type),
			ds.Spec.Source.URI,
		}
		for k, v := range options {
			if regexp.MustCompile(`\s`).MatchString(v) {
				args = append(args, fmt.Sprintf("--options=%s=%q", k, v))
			} else {
				args = append(args, fmt.Sprintf("--options=%s=%s", k, v))
			}
		}
		if ds.Spec.MountOptions.Path != "" {
			args = append(args, fmt.Sprintf("--mount-path=%s", ds.Spec.MountOptions.Path))
		}
		if ds.Spec.MountOptions.Mode != "" {
			args = append(args, fmt.Sprintf("--mount-mode=%s", ds.Spec.MountOptions.Mode))
		}
		args = append(args, fmt.Sprintf("--mount-uid=%d", ds.Spec.MountOptions.UID))
		args = append(args, fmt.Sprintf("--mount-gid=%d", ds.Spec.MountOptions.GID))
		args = append(args, fmt.Sprintf("--mount-root=%s", pvcMountPath))

		container.Args = args

		// 最终创建 Job
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      jobName,
				Namespace: ds.Namespace,
				Labels: lo.Assign(ds.Labels, map[string]string{
					constants.DatasetNameLabel: ds.Name,
				}),
				Annotations:     ds.Annotations,
				OwnerReferences: datasetOwnerRef(ds),
			},
			Spec: jobSpec,
		}
		if err := r.Create(ctx, job); err != nil && !k8serrors.IsAlreadyExists(err) {
			return err
		}
	}

	return nil
}

func (r *DatasetReconciler) reconcileJobStatus(ctx context.Context, ds *datasetv1alpha1.Dataset) error {
	if !supportPreload(ds) {
		ds.Status.LastSyncTime = ds.CreationTimestamp
		return nil
	}
	if !ds.Status.InProcessing {
		lastSucceedRound := ds.Status.LastSucceedRound
		if lastSucceedRound > 0 {
			jobName := genJobName(ds.Name, lastSucceedRound)
			job := &batchv1.Job{}
			if err := r.Get(ctx, client.ObjectKey{Namespace: ds.Namespace, Name: jobName}, job); err != nil {
				return err
			}
			ds.Status.LastSyncTime = lo.FromPtrOr(job.Status.CompletionTime, ds.CreationTimestamp)
		} else {
			ds.Status.LastSyncTime = ds.CreationTimestamp
		}
		return nil
	}

	jobName := genJobName(ds.Name, ds.Status.InProcessingRound)
	job := &batchv1.Job{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: ds.Namespace, Name: jobName}, job); err != nil {
		return err
	}

	_, index, _ := lo.FindIndexOf(ds.Status.SyncRoundStatuses, func(s datasetv1alpha1.DataLoadStatus) bool {
		return s.Round == ds.Status.InProcessingRound
	})
	if index == -1 {
		index = len(ds.Status.SyncRoundStatuses)
		ds.Status.SyncRoundStatuses = append(ds.Status.SyncRoundStatuses, datasetv1alpha1.DataLoadStatus{
			Round:     ds.Status.InProcessingRound,
			JobName:   jobName,
			StartTime: metav1.Time{Time: time.Now()},
			Succeed:   false,
		})
	}
	loader := &ds.Status.SyncRoundStatuses[index]

	if job.Status.Succeeded > 0 {
		loader.StartTime = lo.FromPtrOr(job.Status.StartTime, loader.StartTime)
		loader.EndTime = lo.FromPtrOr(job.Status.CompletionTime, metav1.Time{Time: time.Now()})
		ds.Status.LastSyncTime = lo.FromPtrOr(job.Status.CompletionTime, metav1.Time{Time: time.Now()})
		loader.Succeed = true
		ds.Status.InProcessing = false
		ds.Status.LastSucceedRound = ds.Status.InProcessingRound
		ds.Status.InProcessingRound = 0
	} else if lo.ContainsBy(job.Status.Conditions, func(item batchv1.JobCondition) bool {
		return item.Type == batchv1.JobFailed && item.Status == corev1.ConditionTrue
	}) {
		ds.Status.InProcessing = false
		ds.Status.InProcessingRound = 0
		loader.Succeed = false
	}

	// 滚动清理过期的历史记录
	ds.Status.SyncRoundStatuses = lo.Filter(ds.Status.SyncRoundStatuses, func(item datasetv1alpha1.DataLoadStatus, _ int) bool {
		return item.Round+keepConditions > ds.Spec.DataSyncRound
	})
	return nil
}

func (r *DatasetReconciler) reconcilePhase(_ context.Context, ds *datasetv1alpha1.Dataset) error {
	var phase datasetv1alpha1.DatasetStatusPhase
	switch ds.Spec.Source.Type {
	case datasetv1alpha1.DatasetTypeReference:
		if _, ok := lo.Find(ds.Status.Conditions, func(c metav1.Condition) bool {
			return c.Status == metav1.ConditionFalse
		}); ok {
			ds.Status.Phase = datasetv1alpha1.DatasetStatusPhaseFailed
			return nil
		}
	}

	if ds.Spec.Source.Type == datasetv1alpha1.DatasetTypePVC {
		phase = datasetv1alpha1.DatasetStatusPhaseReady
	} else if ds.Status.InProcessing {
		phase = datasetv1alpha1.DatasetStatusPhaseProcessing
	} else if ds.Status.LastSucceedRound != ds.Spec.DataSyncRound {
		phase = datasetv1alpha1.DatasetStatusPhaseFailed
	} else if ds.Status.LastSucceedRound == ds.Spec.DataSyncRound {
		phase = datasetv1alpha1.DatasetStatusPhaseReady
	} else {
		phase = datasetv1alpha1.DatasetStatusPhasePending
	}

	ds.Status.Phase = phase
	return nil
}

func (r *DatasetReconciler) getSourceDataset(ctx context.Context, ds *datasetv1alpha1.Dataset) (*datasetv1alpha1.Dataset, error) {
	u, err := url.Parse(ds.Spec.Source.URI)
	if err != nil {
		return nil, err
	}
	sourceDs := &datasetv1alpha1.Dataset{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: u.Host, Name: strings.Trim(u.Path, "/")}, sourceDs); err != nil {
		return nil, fmt.Errorf("fetch source dataset %s error: %v", ds.Spec.Source.URI, err)
	}
	return sourceDs, nil
}

func (r *DatasetReconciler) validate(ctx context.Context, ds *datasetv1alpha1.Dataset) error {
	if ds.Spec.Source.Type == datasetv1alpha1.DatasetTypeReference {
		sourceDs, err := r.getSourceDataset(ctx, ds)
		if err != nil {
			return err
		}
		if !sourceDs.Spec.Share {
			return fmt.Errorf("source dataset %s is not shared", ds.Spec.Source.URI)
		}
		if sourceDs.Spec.ShareToNamespaceSelector != nil {
			// 获取当前 Dataset 所在的 Namespace
			currNS := &corev1.Namespace{}
			if err := r.Get(ctx, client.ObjectKey{Name: ds.Namespace}, currNS); err != nil {
				return fmt.Errorf("fetch current namespace %s error: %v", ds.Namespace, err)
			}
			s, err := metav1.LabelSelectorAsSelector(sourceDs.Spec.ShareToNamespaceSelector)
			if err != nil {
				return fmt.Errorf("parse share to namespace selector error: %v", err)
			}
			if !s.Matches(labels.Set(currNS.Labels)) {
				return fmt.Errorf("source dataset %s is not shared to current namespace", ds.Spec.Source.URI)
			}
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DatasetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&datasetv1alpha1.Dataset{}).
		Complete(r)
}
