package kubernetes

import (
	"github.com/ovgu-cs-workshops/cmanager/users"
	"github.com/ovgu-cs-workshops/cmanager/util"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"os"
)

type KubernetesConnector struct {
	clientInstance *kubernetes.Clientset
}

func New() *KubernetesConnector {

	kubeConfig, ok := os.LookupEnv("KUBECONFIG")
	if !ok {
		util.Log.Errorf("Failed to read KUBECONFIG")
		os.Exit(1)
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeConfig)
	if err != nil {
		panic(err.Error())
	}

	clientInstance, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	util.Log.Info("Created KubernetesConnector")

	return &KubernetesConnector{
		clientInstance,
	}

}

func (k *KubernetesConnector) CreatePod(instanceId string, userName string, userPassword string, imageName string) (*v1.Pod, error) {

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
				},
			},
		},
	}

	return k.clientInstance.CoreV1().Pods("cmanager").Create(&podDescription)

}

func (k *KubernetesConnector) ExistingUsers() users.ContainerList {

	listOptions := metav1.ListOptions{}
	util.Log.Info("Checking for existing pods")

	podList, _ := k.clientInstance.CoreV1().Pods("cmanager").List(listOptions)

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
