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

package v1alpha1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DatasetStatusPhase string
type DatasetType string

const (
	DatasetTypeGit         DatasetType = "GIT"
	DatasetTypeS3          DatasetType = "S3"
	DatasetTypePVC         DatasetType = "PVC"
	DatasetTypeNFS         DatasetType = "NFS"
	DatasetTypeHTTP        DatasetType = "HTTP"
	DatasetTypeConda       DatasetType = "CONDA"
	DatasetTypeReference   DatasetType = "REFERENCE"
	DatasetTypeHuggingFace DatasetType = "HUGGING_FACE"
	DatasetTypeModelScope  DatasetType = "MODEL_SCOPE"

	// must be same as apis/management-api/dataset/v1alpha1/dataset.proto
	DatasetStatusPhasePending    DatasetStatusPhase = "PENDING"
	DatasetStatusPhaseReady      DatasetStatusPhase = "READY"
	DatasetStatusPhaseProcessing DatasetStatusPhase = "PROCESSING"
	DatasetStatusPhaseFailed     DatasetStatusPhase = "FAILED"

	// avoid unused error
	_ = DatasetStatusPhasePending
	_ = DatasetStatusPhaseReady
	_ = DatasetStatusPhaseProcessing
	_ = DatasetStatusPhaseFailed
)

type DatasetSource struct {
	// +kubebuilder:validation:Enum=GIT;S3;HTTP;PVC;NFS;CONDA;REFERENCE;HUGGING_FACE;MODEL_SCOPE
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Value is immutable"
	Type DatasetType `json:"type"`
	// +kubebuilder:validation:Required
	// uri is the location of the dataset.
	// each type of dataset source has its own format of uri:
	// - GIT: http[s]://<host>/<owner>/<repo>[.git] or git://<host>/<owner>/<repo>[.git]
	// - S3: s3://<bucket>/<path/to/directory>
	// - HTTP: http[s]://<host>/<path/to/directory>?<query>
	// - PVC: pvc://<name>/<path/to/directory>
	// - NFS: nfs://<host>/<path/to/directory>
	// - CONDA: conda://<name>?[python=<python_version>]
	// - REFERENCE: dataset://<namespace>/<dataset>
	// - HUGGING_FACE: huggingface://<repoName>?[repoType=<repoType>]
	// - MODEL_SCOPE: modelscope://<namespace>/<model>
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Value is immutable"
	URI string `json:"uri"`
	// +kubebuilder:validation:Optional
	// +kubebuilder:pruning:PreserveUnknownFields
	// options is a map of key-value pairs that can be used to specify additional options for the dataset source, e.g. {"branch": "master"}
	// supported keys for each type of dataset source are:
	// - GIT: branch, commit, depth, submodules
	// - S3: region, endpoint, provider, syncMode
	// - HTTP: any key-value pair will be passed to the underlying http client as http headers
	// - PVC:
	// - NFS:
	// - CONDA: requirements.txt, environment.yaml
	// - REFERENCE:
	// - HUGGING_FACE: repo, repoType, endpoint, include, exclude, revision
	// - MODEL_SCOPE: repo, repoType, include, exclude, revision
	// * Note: syncMode can be "sync" (default) or "copy". "sync" removes files in destination that don't exist in source, "copy" only adds/updates files without removing existing ones.
	Options map[string]string `json:"options,omitempty"`
}

type MountOptions struct {
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="/"
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Value is immutable"
	// path is the path to the directory to be mounted.
	// if set to "/", the dataset will be mounted to the root of the dest volume.
	// if set to a non-empty string, the dataset will be mounted to a subdirectory of the dest volume.
	Path string `json:"path,omitempty"`
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="0774"
	// +kubebuilder:validation:Pattern="^[0-7]{3,4}$"
	// mode is the permission mode of the mounted directory.
	Mode string `json:"mode,omitempty"`
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=1000
	// uid is the user id of the mounted directory.
	UID int64 `json:"uid,omitempty"`
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=1000
	// gid is the group id of the mounted directory.
	GID int64 `json:"gid,omitempty"`
}

// DatasetSpec defines the desired state of Dataset
type DatasetSpec struct {
	// Share indicates whether the model is shareable with others.
	// When set to true, the model can be shared according to the specified selector.
	// +kubebuilder:validation:Optional
	Share bool `json:"share,omitempty"`
	// ShareToNamespaceSelector defines a label selector to specify the namespaces
	// to which the model can be shared. Only namespaces that match the selector will have access to the model.
	// If Share is true and ShareToNamespaceSelector is empty, that means all namespaces can access this.
	// +kubebuilder:validation:Optional
	ShareToNamespaceSelector *metav1.LabelSelector `json:"shareToNamespaceSelector,omitempty"`
	// +kubebuilder:validation:Required
	// source is the source of the dataset.
	Source DatasetSource `json:"source"`
	// +kubebuilder:validation:Optional
	// secretRef is the name of the secret that contains credentials for accessing the dataset source.
	SecretRef string `json:"secretRef,omitempty"`
	// +kubebuilder:validation:Optional
	// mountOptions is the options for mounting the dataset.
	MountOptions MountOptions `json:"mountOptions,omitempty"`
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:XValidation:rule="self > 0 && self - oldSelf <= 1",message="dataSyncRound can only be incremented by 1"
	// +kubebuilder:default=1
	// dataSyncRound is the number of data sync rounds to be performed."
	DataSyncRound int32 `json:"dataSyncRound,omitempty"`
	// +kubebuilder:validation:Optional
	VolumeClaimTemplate v1.PersistentVolumeClaim `json:"volumeClaimTemplate,omitempty"`
}

type DataLoadStatus struct {
	// +kubebuilder:validation:Optional
	Round int32 `json:"round,omitempty"`
	// +kubebuilder:validation:Optional
	JobName string `json:"jobName,omitempty"`
	// +kubebuilder:validation:Optional
	StartTime metav1.Time `json:"startTime,omitempty"`
	// +kubebuilder:validation:Optional
	EndTime metav1.Time `json:"endTime,omitempty"`
	// +kubebuilder:validation:Optional
	Succeed bool `json:"succeed,omitempty"`
}

// DatasetStatus defines the observed state of Dataset
type DatasetStatus struct {
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=PENDING
	Phase DatasetStatusPhase `json:"phase,omitempty"`
	// +kubebuilder:validation:Optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// +kubebuilder:validation:Optional
	InProcessing bool `json:"inProcessing,omitempty"`
	// +kubebuilder:validation:Optional
	InProcessingRound int32 `json:"inProcessingRound,omitempty"`
	// +kubebuilder:validation:Optional
	// lastSucceedRound is the number of the last data sync round.
	LastSucceedRound int32 `json:"lastSucceedRound,omitempty"`
	// +kubebuilder:validation:Optional
	// syncRoundStatuses is a list of data sync round statuses.
	// each data sync round status contains the information of the data sync round.
	// we only keep the data sync round statuses of the last 5 data sync rounds.
	SyncRoundStatuses []DataLoadStatus `json:"syncRoundStatuses,omitempty"`
	// +kubebuilder:validation:Optional
	// pvcName is the name of the pvc that contains the dataset.
	PVCName string `json:"pvcName,omitempty"`
	// +kubebuilder:validation:Optional
	// readOnly indicates whether the dataset is mounted as read-only.
	ReadOnly     bool        `json:"readOnly,omitempty"`
	LastSyncTime metav1.Time `json:"lastSyncTime,omitempty"`
	// SourceType defaults to spec.source.type, unless it is a REFERENCE,
	// in which case it will match the type of the original Dataset.
	SourceType DatasetType `json:"sourceType,omitempty"`
}

// Dataset is the Schema for the datasets API
// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=data
// +kubebuilder:printcolumn:name="type",type=string,JSONPath=`.status.sourceType`
// +kubebuilder:printcolumn:name="uri",type=string,JSONPath=`.spec.source.uri`
// +kubebuilder:printcolumn:name="phase",type=string,JSONPath=`.status.phase`
type Dataset struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DatasetSpec   `json:"spec,omitempty"`
	Status DatasetStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// DatasetList contains a list of Dataset
type DatasetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Dataset `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Dataset{}, &DatasetList{})
}
