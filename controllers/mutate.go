package controllers

import (
	"fmt"
	"strconv"

	databasev1 "github.com/erda-project/mysql-operator/api/v1"
	"github.com/erda-project/mysql-operator/pkg/mylet"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
)

var (
	PersistentVolumeBlock      = corev1.PersistentVolumeBlock
	PersistentVolumeFilesystem = corev1.PersistentVolumeFilesystem
)

func MutateSts(mysql *databasev1.Mysql, sts *appsv1.StatefulSet) {
	labels := mysql.NewLabels()
	podLables := make(map[string]string, len(labels)+len(mysql.Spec.Labels))
	for k, v := range mysql.Spec.Labels {
		podLables[k] = v
	}
	for k, v := range labels {
		podLables[k] = v
	}

	annotations := make(map[string]string, len(mysql.Spec.Annotations))
	for k, v := range mysql.Spec.Annotations {
		annotations[k] = v
	}

	sts.Spec = appsv1.StatefulSetSpec{
		ServiceName: mysql.BuildName(databasev1.HeadlessSuffix),
		Replicas:    pointer.Int32Ptr(int32(mysql.Spec.Size())),
		Selector: &metav1.LabelSelector{
			MatchLabels: labels,
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      podLables,
				Annotations: annotations,
			},
			Spec: corev1.PodSpec{
				Affinity: mysql.Spec.Affinity.DeepCopy(),
				InitContainers: []corev1.Container{
					{
						Name: "init",
						Command: []string{
							"bash",
							"-c",
							"mkdir -p /mydir/my.cnf.d && chown mysql:mysql /mydir /mydir/my.cnf.d",
						},
						Image:           mysql.Spec.Image,
						ImagePullPolicy: mysql.Spec.ImagePullPolicy,
						SecurityContext: &corev1.SecurityContext{
							RunAsUser: pointer.Int64Ptr(0),
						},
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "mydir",
								MountPath: "/mydir",
							},
						},
						Env: NewEnv(),
					},
				},
				Containers: []corev1.Container{
					{
						Name:            "mylet",
						Image:           mysql.Spec.Image,
						ImagePullPolicy: mysql.Spec.ImagePullPolicy,
						Resources:       mysql.Spec.Resources,
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "mydir",
								MountPath: "/mydir",
							},
						},
						Env: NewEnv(
							corev1.EnvVar{
								Name:  "MYCTL_ADDR",
								Value: mysql.Spec.MyctlAddr,
							},
							corev1.EnvVar{
								Name:  "GROUP_TOKEN",
								Value: mylet.GroupToken(mysql),
							},
							corev1.EnvVar{
								Name:  "HTTP_ADDR",
								Value: ":" + strconv.Itoa(mysql.Spec.MyletPort),
							},
						),
						/*TODO
						Lifecycle: &corev1.Lifecycle{
							PostStart: &corev1.LifecycleHandler{
								HTTPGet: &corev1.HTTPGetAction{
									Path:   "/api/addons/mylet/post/start",
									Port:   intstr.FromInt(mysql.Spec.MyletPort),
									Scheme: corev1.URISchemeHTTP,
								},
							},
							PreStop: &corev1.LifecycleHandler{
								HTTPGet: &corev1.HTTPGetAction{
									Path:   "/api/addons/mylet/pre/stop",
									Port:   intstr.FromInt(mysql.Spec.MyletPort),
									Scheme: corev1.URISchemeHTTP,
								},
							},
						},
						*/
						StartupProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								HTTPGet: &corev1.HTTPGetAction{
									Path:   "/api/addons/mylet/probe/startup",
									Port:   intstr.FromInt(mysql.Spec.MyletPort),
									Scheme: corev1.URISchemeHTTP,
								},
							},
							// 1h
							FailureThreshold:    720,
							InitialDelaySeconds: 1,
							PeriodSeconds:       5,
							SuccessThreshold:    1,
							TimeoutSeconds:      1,
						},
						LivenessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								HTTPGet: &corev1.HTTPGetAction{
									Path:   "/api/addons/mylet/probe/liveness",
									Port:   intstr.FromInt(mysql.Spec.MyletPort),
									Scheme: corev1.URISchemeHTTP,
								},
							},
							// 1m
							FailureThreshold:    12,
							InitialDelaySeconds: 1,
							PeriodSeconds:       5,
							SuccessThreshold:    1,
							TimeoutSeconds:      1,
						},
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								HTTPGet: &corev1.HTTPGetAction{
									Path:   "/api/addons/mylet/probe/readiness",
									Port:   intstr.FromInt(mysql.Spec.MyletPort),
									Scheme: corev1.URISchemeHTTP,
								},
							},
							// 1m
							FailureThreshold:    12,
							InitialDelaySeconds: 1,
							PeriodSeconds:       5,
							SuccessThreshold:    1,
							TimeoutSeconds:      1,
						},
					},
				},
			},
		},
		VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "mydir",
					Namespace: mysql.Namespace,
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: mysql.Spec.StorageSize,
						},
					},
					StorageClassName: &mysql.Spec.StorageClassName,
					VolumeMode:       &PersistentVolumeFilesystem,
				},
			},
		},
	}

	if mysql.Spec.EnableExporter {
		dsn := fmt.Sprintf("%s:%s@tcp(localhost:%d)/",
			mysql.Spec.ExporterUsername, mysql.Spec.ExporterPassword, mysql.Spec.Port)

		sts.Spec.Template.Spec.Containers = append(sts.Spec.Template.Spec.Containers, corev1.Container{
			Name:            "exporter",
			Image:           mysql.Spec.ExporterImage,
			ImagePullPolicy: mysql.Spec.ImagePullPolicy,
			Args: append([]string{
				"--web.listen-address=:" + strconv.Itoa(mysql.Spec.ExporterPort),
			}, mysql.Spec.ExporterFlags...),
			Env: NewEnv(
				corev1.EnvVar{
					Name:  "DATA_SOURCE_NAME",
					Value: dsn,
				},
			),
		})
	}
}

func NewEnv(a ...corev1.EnvVar) []corev1.EnvVar {
	return append([]corev1.EnvVar{
		{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
		},
		{
			Name: "NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.namespace",
				},
			},
		},
		{
			Name: "NODE_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "spec.nodeName",
				},
			},
		},
		{
			Name: "HOST_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "status.hostIP",
				},
			},
		},
		{
			Name: "POD_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "status.podIP",
				},
			},
		},
	}, a...)
}

func MutateSvc(mysql *databasev1.Mysql, svc *corev1.Service, x string) {
	svc.Labels = mysql.NewLabels()
	svc.Spec = corev1.ServiceSpec{
		Selector: mysql.NewLabels(),
		Ports: []corev1.ServicePort{
			{
				Name:       "mysqld",
				Protocol:   corev1.ProtocolTCP,
				Port:       int32(mysql.Spec.Port),
				TargetPort: intstr.FromInt(mysql.Spec.Port),
			},
			{
				Name:       "mylet",
				Protocol:   corev1.ProtocolTCP,
				Port:       int32(mysql.Spec.MyletPort),
				TargetPort: intstr.FromInt(mysql.Spec.MyletPort),
			},
		},
	}

	if mysql.Spec.EnableExporter {
		svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{
			Name:       "exporter",
			Protocol:   corev1.ProtocolTCP,
			Port:       int32(mysql.Spec.ExporterPort),
			TargetPort: intstr.FromInt(mysql.Spec.ExporterPort),
		})
	}

	switch x {
	case databasev1.HeadlessSuffix:
		svc.Spec.ClusterIP = corev1.ClusterIPNone
	case "write":
		svc.Spec.Selector["statefulset.kubernetes.io/pod-name"] = mysql.SoloName(*mysql.Status.WriteId)
	case "read":
		svc.Spec.Selector["statefulset.kubernetes.io/pod-name"] = mysql.SoloName(*mysql.Status.ReadId)
	}
}
