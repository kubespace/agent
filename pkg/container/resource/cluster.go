package resource

import (
	"github.com/kubespace/agent/pkg/kubernetes"
	"github.com/kubespace/agent/pkg/utils"
	"github.com/kubespace/agent/pkg/utils/code"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog"
)

type Cluster struct {
	*kubernetes.KubeClient
	watch *WatchResource
	*DynamicResource
}

func NewCluster(kubeClient *kubernetes.KubeClient, watch *WatchResource) *Cluster {
	c := &Cluster{
		watch:           watch,
		KubeClient:      kubeClient,
		DynamicResource: NewDynamicResource(kubeClient, nil),
	}
	//c.DoWatch()
	return c
}

type BuildCluster struct {
	ClusterVersion  string `json:"cluster_version"`
	ClusterCpu      string `json:"cluster_cpu"`
	ClusterMemory   string `json:"cluster_memory"`
	NodeNum         int    `json:"node_num"`
	NamespaceNum    int    `json:"namespace_num"`
	PodNum          int    `json:"pod_num"`
	PodRunningNum   int    `json:"pod_running_num"`
	PodSucceededNum int    `json:"pod_succeeded_num"`
	PodPendingNum   int    `json:"pod_pending_num"`
	PodFailedNum    int    `json:"pod_failed_num"`
	DeploymentNum   int    `json:"deployment_num"`
	StatefulSetNum  int    `json:"statefulset_num"`
	DaemonSetNum    int    `json:"daemonset_num"`
	ServiceNum      int    `json:"service_num"`
	IngressNum      int    `json:"ingress_num"`
	StorageClassNum int    `json:"storageclass_num"`
	PVNum           int    `json:"pv_num"`
	PVAvailableNum  int    `json:"pv_available_num"`
	PVReleasedNum   int    `json:"pv_released_num"`
	PVBoundNum      int    `json:"pv_bound_num"`
	PVFailedNum     int    `json:"pv_failed_num"`
	PVCNum          int    `json:"pvc_num"`
}

type ClusterQueryParams struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	UID       string `json:"uid"`
	Output    string `json:"output"`
}

func (c *Cluster) Get(requestParams interface{}) *utils.Response {
	var bc = BuildCluster{}
	version, err := c.ClientSet.Discovery().ServerVersion()
	if err != nil {
		klog.Errorf("get version error: %s", err)
		return &utils.Response{Code: code.ListError, Msg: err.Error()}
	}
	bc.ClusterVersion = version.GitVersion

	nodes, err := c.KubeClient.NodeInformer().Lister().List(labels.Everything())
	if err != nil {
		return &utils.Response{Code: code.ListError, Msg: err.Error()}
	}
	bc.NodeNum = len(nodes)
	var cpu resource.Quantity
	var memory resource.Quantity
	for _, n := range nodes {
		cpu.Add(*n.Status.Capacity.Cpu())
		memory.Add(*n.Status.Capacity.Memory())
	}
	bc.ClusterCpu = cpu.String()
	bc.ClusterMemory = memory.String()
	namespaces, err := c.KubeClient.NamespaceInformer().Lister().List(labels.Everything())
	if err != nil {
		return &utils.Response{Code: code.ListError, Msg: err.Error()}
	}
	bc.NamespaceNum = len(namespaces)
	pods, err := c.KubeClient.PodInformer().Lister().List(labels.Everything())
	if err != nil {
		return &utils.Response{Code: code.ListError, Msg: err.Error()}
	}
	bc.PodNum = len(pods)
	for _, p := range pods {
		if p.Status.Phase == corev1.PodRunning {
			bc.PodRunningNum += 1
		} else if p.Status.Phase == corev1.PodPending {
			bc.PodPendingNum += 1
		} else if p.Status.Phase == corev1.PodFailed {
			bc.PodFailedNum += 1
		} else if p.Status.Phase == corev1.PodSucceeded {
			bc.PodSucceededNum += 1
		}
	}
	deployments, err := c.KubeClient.DeploymentInformer().Lister().List(labels.Everything())
	if err != nil {
		return &utils.Response{Code: code.ListError, Msg: err.Error()}
	}
	bc.DeploymentNum = len(deployments)
	statefulsets, err := c.KubeClient.StatefulSetInformer().Lister().List(labels.Everything())
	if err != nil {
		return &utils.Response{Code: code.ListError, Msg: err.Error()}
	}
	bc.StatefulSetNum = len(statefulsets)
	daemonsets, err := c.KubeClient.DaemonSetInformer().Lister().List(labels.Everything())
	if err != nil {
		return &utils.Response{Code: code.ListError, Msg: err.Error()}
	}
	bc.DaemonSetNum = len(daemonsets)
	services, err := c.KubeClient.ServiceInformer().Lister().List(labels.Everything())
	if err != nil {
		return &utils.Response{Code: code.ListError, Msg: err.Error()}
	}
	bc.ServiceNum = len(services)
	ingresses, err := c.KubeClient.IngressInformer().Lister().List(labels.Everything())
	if err != nil {
		return &utils.Response{Code: code.ListError, Msg: err.Error()}
	}
	bc.IngressNum = len(ingresses)
	sc, err := c.KubeClient.StorageClassInformer().Lister().List(labels.Everything())
	if err != nil {
		return &utils.Response{Code: code.ListError, Msg: err.Error()}
	}
	bc.StorageClassNum = len(sc)
	pv, err := c.KubeClient.PersistentVolumeInformer().Lister().List(labels.Everything())
	if err != nil {
		return &utils.Response{Code: code.ListError, Msg: err.Error()}
	}
	bc.PVNum = len(pv)
	for _, p := range pv {
		if p.Status.Phase == corev1.VolumeAvailable {
			bc.PVAvailableNum += 1
		} else if p.Status.Phase == corev1.VolumeBound {
			bc.PVBoundNum += 1
		} else if p.Status.Phase == corev1.VolumeReleased {
			bc.PVReleasedNum += 1
		} else if p.Status.Phase == corev1.VolumeFailed {
			bc.PVFailedNum += 1
		}
	}
	pvc, err := c.KubeClient.PersistentVolumeClaimInformer().Lister().List(labels.Everything())
	if err != nil {
		return &utils.Response{Code: code.ListError, Msg: err.Error()}
	}
	bc.PVCNum = len(pvc)
	return &utils.Response{Code: code.Success, Msg: "Success", Data: bc}
}
