package kubernetes

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/gammazero/nexus/wamp"
	"github.com/ovgu-cs-workshops/cmanager/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9]{1,32}$`)

func EnvOrDefault(envName, defaultValue string) string {
	if v, ok := os.LookupEnv(envName); ok {
		return v
	}
	return defaultValue
}

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

func (k *KubernetesConnector) WatchPVC(stop chan struct{}) error {
	listOptions := metav1.ListOptions{
		LabelSelector: "git-talk=true",
	}
	podNamespace := os.Getenv("POD_NAMESPACE")
	for {
		watch, err := k.clientInstance.CoreV1().PersistentVolumeClaims(podNamespace).Watch(listOptions)
		if err != nil {
			return err
		}
		results := watch.ResultChan()
		util.Log.Infof("Established pvc watch")
	inner:
		for {
			select {
			case <-stop:
				return nil
			case evt, ok := <-results:
				if !ok {
					util.Log.Warningf("APIServer ended our PVC watch, establishing a new watch.")
					break inner
				}
				pvc, ok := evt.Object.(*v1.PersistentVolumeClaim)
				if !ok {
					util.Log.Errorf("Expected PVC, got %v", evt.Object)
					continue
				}
				instance, ok := pvc.Annotations["git-talk-inst"]
				if !ok {
					util.Log.Warningf("Missing instance annotation for pvc %s", pvc.Name)
					continue
				}
				util.Log.Debugf("PVC for instance %s is now %s", instance, pvc.Status.Phase)
				if pvc.Status.Phase == "Bound" {
					go func() {
						// best effort
						util.App.Client.Publish(fmt.Sprintf("rocks.git.%s.state", instance), nil, wamp.List{"pvcbound"}, nil)
					}()
				}
			}
		}
		time.Sleep(2 * time.Second)
	}
}

func (k *KubernetesConnector) WatchPod(stop chan struct{}) error {
	listOptions := metav1.ListOptions{
		LabelSelector: "git-talk=true",
	}
	podNamespace := os.Getenv("POD_NAMESPACE")
	for {
		watch, err := k.clientInstance.CoreV1().Pods(podNamespace).Watch(listOptions)
		if err != nil {
			return err
		}
		results := watch.ResultChan()
		util.Log.Infof("Established pod watch")
	inner:
		for {
			select {
			case <-stop:
				return nil
			case evt, ok := <-results:
				if !ok {
					util.Log.Warningf("APIServer ended our pod watch, establishing a new one")
					break inner
				}
				pod, ok := evt.Object.(*v1.Pod)
				if !ok {
					util.Log.Errorf("Expected Pod, got %v", evt.Object)
					continue
				}
				instance, ok := pod.Annotations["git-talk-inst"]
				if !ok {
					util.Log.Warningf("Missing instance annotation for pod %s", pod.Name)
					continue
				}
				util.Log.Debugf("Pod for instance %s is now %s", instance, pod.Status.Phase)
				if pod.Status.Phase == "Running" {
					go func() {
						// best effort
						util.App.Client.Publish(fmt.Sprintf("rocks.git.%s.state", instance), nil, wamp.List{"podrunning"}, nil)
					}()
				}
			}
		}
		time.Sleep(2 * time.Second)
	}
}

func (k *KubernetesConnector) StartEnvironment(userName string, userPassword string, imageName string) (*v1.Pod, error) {
	if !usernameRegex.MatchString(userName) {
		return nil, fmt.Errorf("invalid username, must be 1-32 alphanumeric characters")
	}
	podNamespace := os.Getenv("POD_NAMESPACE")
	storageClass := os.Getenv("POD_STORAGE_CLASS")

	pod, _, _, ok := k.FindPodForUser(userName, &userPassword)
	if ok {
		return pod, nil;
	}
	var instanceId string
	if inst, err := util.RandomHex(4); err != nil {
		return nil, errors.New("failed to generate instance id")
	} else {
		instanceId = inst
	}

	pvDescription := v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "userland-" + instanceId + "-home",
			Labels: map[string]string{
				"git-talk": "true",
			},
			Annotations: map[string]string{
				"git-talk-inst": instanceId,
			},
		},
		Spec: v1.PersistentVolumeClaimSpec{
			StorageClassName: &storageClass,
			AccessModes:      []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
			Resources: v1.ResourceRequirements{
				Requests: v1.ResourceList{
					v1.ResourceName(v1.ResourceStorage): resource.MustParse("100Mi"),
				},
			},
		},
	}

	_, err := k.clientInstance.CoreV1().PersistentVolumeClaims(podNamespace).Create(&pvDescription)

	if err != nil {
		return nil, err
	}

	svcName, hasSvc := os.LookupEnv("SERVICE_ACCOUNT")
	var svcAcct *string
	if hasSvc {
		svcAcct = &svcName
	}

	privileged := false
	if privilegedFlag, flagOk := os.LookupEnv("RUNPRIVILEGED"); flagOk {
		if privilegedParse, err := strconv.ParseBool(privilegedFlag); err == nil {
			privileged = privilegedParse
		} else {
			util.Log.Warningf("RUNPRIVILEGED was specified but unable to parse, fallback to false! %v", err)
		}
	}

	podDescription := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "userland-" + instanceId,
			Labels: map[string]string{
				"git-talk":      "true",
				"git-talk-user": userName,
			},
			Annotations: map[string]string{
				"git-talk-pass": userPassword,
				"git-talk-inst": instanceId,
			},
		},
		Spec: v1.PodSpec{
			Affinity: &v1.Affinity{
				PodAffinity: &v1.PodAffinity{
					PreferredDuringSchedulingIgnoredDuringExecution: []v1.WeightedPodAffinityTerm{
						{
							Weight: 1,
							PodAffinityTerm: v1.PodAffinityTerm{
								TopologyKey: "kubernetes.io/hostname",
								LabelSelector: &metav1.LabelSelector{
									MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key:      "git-talk-service",
											Operator: metav1.LabelSelectorOpIn,
											Values:   []string{"broker"},
										},
									},
								},
							},
						},
					},
				},
			},
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
			AutomountServiceAccountToken: &hasSvc,
			ServiceAccountName: *svcAcct,
			Containers: []v1.Container{
				{
					Name:  "userland-" + instanceId,
					Image: imageName,
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							"cpu":    resource.MustParse(EnvOrDefault("CPU_REQUEST", "500m")),
							"memory": resource.MustParse(EnvOrDefault("MEM_REQUEST", "512Mi")),
						},
						Limits: v1.ResourceList{
							"cpu":    resource.MustParse(EnvOrDefault("CPU_LIMIT", "1")),
							"memory": resource.MustParse(EnvOrDefault("MEM_LIMIT", "1Gi")),
						},
					},
					SecurityContext: &v1.SecurityContext{
						Privileged: &privileged,
					},
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

func (k *KubernetesConnector) FindPodForUser(userName string, ticket *string) (*v1.Pod, string, bool, bool) {
	if !usernameRegex.MatchString(userName) {
		return nil, "", false, false
	}
	util.Log.Debug("Searching pod for user %s", userName)

	listOptions := metav1.ListOptions{
		LabelSelector: fmt.Sprintf("git-talk=true,git-talk-user=%s", userName),
	}
	podNamespace := os.Getenv("POD_NAMESPACE")
	podList, _ := k.clientInstance.CoreV1().Pods(podNamespace).List(listOptions)
	util.Log.Debugf("Found %d pods for user %s", len(podList.Items), userName)
	for _, pod := range podList.Items {
		uPass, passOk := pod.Annotations["git-talk-pass"]
		uInst, instOk := pod.Annotations["git-talk-inst"]
		if !passOk || !instOk {
			util.Log.Warningf("Corrupt pod annotations on pod %s", pod.Name)
			continue
		}
		if ticket == nil || uPass == *ticket {
			// ticket=nil means, we're called from the authorizer, so no need to check it
			readyToUse := pod.Status.Phase == "Running"
			return &pod, uInst, readyToUse, true
		}
	}
	return nil, "", false, false
}
