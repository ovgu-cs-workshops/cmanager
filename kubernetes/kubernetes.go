package kubernetes

import (
	"github.com/ovgu-cs-workshops/cmanager/users"
	"github.com/ovgu-cs-workshops/cmanager/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"os"
)

type KubernetesConnector struct {
	clientInstance *kubernetes.Clientset
}

func New() *KubernetesConnector {

	kubeConfig, ok := os.LookupEnv("KUBECONFIG")
	var clusterConfiguration *rest.Config
	var err error

	if !ok {
		util.Log.Info("KUBECONFIG was not found in environment. Performing In-Cluster-Authentication..")

		clusterConfiguration, err = rest.InClusterConfig()

		if err != nil {
			panic(err.Error())
		}

	} else {
		util.Log.Info("KUBECONFIG was found in environment. Performing External-Cluster-Authentication..")

		clusterConfiguration, err = clientcmd.BuildConfigFromFlags("", kubeConfig)
		if err != nil {
			panic(err.Error())
		}

	}

	clientInstance, err := kubernetes.NewForConfig(clusterConfiguration)
	if err != nil {
		panic(err.Error())
	}

	util.Log.Info("Created KubernetesConnector")

	return &KubernetesConnector{
		clientInstance,
	}

}

func (k *KubernetesConnector) CreatePod(instanceId string, userName string, userPassword string, imageName string) (*v1.Pod, error) {

	volumeMode := v1.PersistentVolumeFilesystem

	pvDescription := v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "userland-" + instanceId + "-home",
		},
		Spec: v1.PersistentVolumeClaimSpec{
			StorageClassName: "ssd-storage",
			AccessModes:      []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
			Resources: v1.ResourceRequirements{
				Requests: v1.ResourceList{
					v1.ResourceName(v1.ResourceStorage): resource.MustParse("100Mi"),
				},
			},
			VolumeName: "userland-" + instanceId + "-home",
			VolumeMode: &volumeMode,
		},
	}

	podNamespace := os.Getenv("POD_NAMESPACE")

	_, err := k.clientInstance.CoreV1().PersistentVolumeClaims(podNamespace).Create(&pvDescription)

	if err != nil {
		return nil, err
	}

	podDescription := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "userland-" + instanceId,
			Annotations: map[string]string{
				"git-talk":      "true",
				"git-talk-user": userName,
				"git-talk-pass": userPassword,
				"git-talk-inst": instanceId,
			},
		},
		Spec: v1.PodSpec{
			Volumes: []v1.Volume{
				{
					Name: "userland-" + instanceId + "-home",
					VolumeSource: v1.VolumeSource{
						PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
							ClaimName: "userland-" + instanceId + "-home",
						},
					},
				},
			},
			Containers: []v1.Container{
				{
					Name:  "userland-" + instanceId,
					Image: imageName,
					Env: []v1.EnvVar{
						{
							Name:  "SERVICE_REALM",
							Value: os.Getenv("SERVICE_REALM"),
						},
						{
							Name:  "SERVICE_BROKER_URL",
							Value: os.Getenv("SERVICE_BROKER_URL"),
						},
						{
							Name:  "RUNUSER",
							Value: userName,
						},
						{
							Name:  "RUNINST",
							Value: instanceId,
						},
					},
					VolumeMounts: []v1.VolumeMount{
						{
							Name:      "userland-" + instanceId + "-home",
							MountPath: "/home/user",
						},
					},
				},
			},
		},
	}

	return k.clientInstance.CoreV1().Pods(podNamespace).Create(&podDescription)

}

func (k *KubernetesConnector) ExistingUsers() users.ContainerList {

	listOptions := metav1.ListOptions{}
	util.Log.Info("Checking for existing pods")

	podNamespace := os.Getenv("POD_NAMESPACE")

	podList, _ := k.clientInstance.CoreV1().Pods(podNamespace).List(listOptions)

	containerInfo := make(users.ContainerList)

	for _, pod := range podList.Items {

		if pod.Annotations["git-talk"] != "true" {
			continue
		}

		uName, nameOk := pod.Annotations["git-talk-user"]
		uPass, passOk := pod.Annotations["git-talk-pass"]
		uInst, instOk := pod.Annotations["git-talk-inst"]

		if !nameOk || !passOk || !instOk {
			util.Log.Warningf("Corrupt Pod Annotations")

		}

		containerInfo[uName] = &users.ContainerInfo{
			Ticket:
			uPass,
			ContainerID: uInst,
		}

		util.Log.Infof("%v", pod.Annotations)
		util.Log.Infof("Found existing pod %v for user %v", uInst, uName)

	}

	return containerInfo

}
