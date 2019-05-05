package kubernetes

import (
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
			Labels: map[string]string{
				"git-talk":      "true",
				"git-talk-user": userName,
				// "git-talk-pass": userPassword,
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

func (k *KubernetesConnector) ExistingPods() {

	listOptions := metav1.ListOptions{}

	podList, _ := k.clientInstance.CoreV1().Pods("cmanager").List(listOptions)

	for _, pod := range podList.Items {
		util.Log.Infof("Name: %v, Labels: %v", pod.Name, pod.Labels)
	}

}
