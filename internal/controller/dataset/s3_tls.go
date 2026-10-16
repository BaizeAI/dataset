package dataset

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/BaizeAI/dataset/pkg/s3tls"
)

// configureS3TLS operates on the copied options only. Kubernetes reads Secrets.
func configureS3TLS(options map[string]string, pod *corev1.PodSpec, container *corev1.Container) error {
	mode, err := s3tls.Mode(options["tlsMode"])
	if err != nil {
		return err
	}
	name := options["tlsCASecretName"]
	if mode == s3tls.CustomCA && name == "" {
		return fmt.Errorf("customCA requires tlsCASecretName")
	}
	if mode != s3tls.CustomCA && name != "" {
		return fmt.Errorf("tlsCASecretName is only allowed with customCA")
	}
	delete(options, "tlsCASecretName")
	delete(options, "tlsCAFile")
	options["tlsMode"] = mode
	if mode != s3tls.CustomCA {
		return nil
	}
	const volume = "dataset-s3-tls"
	const dir = "/run/dataset/s3-tls"
	pod.Volumes = append(pod.Volumes, corev1.Volume{
		Name: volume,
		VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
			SecretName: name,
			Items:      []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}},
		}},
	})
	container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{Name: volume, MountPath: dir, ReadOnly: true})
	options["tlsCAFile"] = dir + "/ca.crt"
	return nil
}
