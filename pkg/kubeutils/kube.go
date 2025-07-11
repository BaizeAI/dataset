package kubeutils

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func IsDeleted(obj client.Object) bool {
	return obj.GetDeletionTimestamp() != nil
}

func IsConditionReady(conditions []metav1.Condition, cond string) bool {
	return lo.ContainsBy(conditions, func(item metav1.Condition) bool {
		return item.Type == cond && item.Status == metav1.ConditionTrue
	})
}

func SetCondition(conditions []metav1.Condition, typ string, err error) []metav1.Condition {
	if typ == "" {
		return conditions
	}
	index := -1
	for i, c := range conditions {
		if c.Type == typ {
			index = i
			break
		}
	}
	if index == -1 {
		index = len(conditions)
		conditions = append(conditions, metav1.Condition{
			Type:   typ,
			Reason: typ + "Ready",
		})
	}
	if err == nil {
		if conditions[index].Status != metav1.ConditionTrue {
			conditions[index].Status = metav1.ConditionTrue
			conditions[index].LastTransitionTime = metav1.Time{Time: time.Now()}
			conditions[index].Message = ""
		}
	} else {
		if conditions[index].Status != metav1.ConditionFalse {
			conditions[index].Status = metav1.ConditionFalse
			conditions[index].LastTransitionTime = metav1.Time{Time: time.Now()}
			conditions[index].Message = err.Error()
		}
	}
	return conditions
}

func GetTolerationWithSeconds(TolerationSeconds *int64) []corev1.Toleration {
	if lo.FromPtr(TolerationSeconds) == 0 {
		return nil
	}
	return []corev1.Toleration{
		{
			Key:               corev1.TaintNodeNotReady,
			Effect:            corev1.TaintEffectNoExecute,
			Operator:          corev1.TolerationOpExists,
			TolerationSeconds: TolerationSeconds,
		},
		{
			Key:               corev1.TaintNodeUnreachable,
			Effect:            corev1.TaintEffectNoExecute,
			Operator:          corev1.TolerationOpExists,
			TolerationSeconds: TolerationSeconds,
		},
	}
}

func GetTolerationSeconds(tolerations []corev1.Toleration) *int64 {
	for index := range tolerations {
		if tolerations[index].Key == corev1.TaintNodeNotReady || tolerations[index].Key == corev1.TaintNodeUnreachable {
			return tolerations[index].TolerationSeconds
		}
	}
	return nil
}

func MapToSelector(labels map[string]string) string {
	es := lo.Entries(labels)
	sort.Slice(es, func(i, j int) bool {
		return es[i].Key < es[j].Key
	})
	return strings.Join(lo.Map(es, func(item lo.Entry[string, string], index int) string {
		if item.Value != "" {
			return fmt.Sprintf("%s=%s", item.Key, item.Value)
		} else {
			return item.Key
		}
	}), ",")
}

type PodReplicaSpec struct {
	Replicas int64
	PodSpec  corev1.PodSpec
}
