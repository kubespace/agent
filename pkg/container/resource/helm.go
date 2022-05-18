package resource

import (
	"bytes"
	"encoding/json"
	"github.com/kubespace/agent/pkg/kubernetes"
	"github.com/kubespace/agent/pkg/ospserver"
	"github.com/kubespace/agent/pkg/utils"
	"github.com/kubespace/agent/pkg/utils/code"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/release"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/klog"
	"sigs.k8s.io/yaml"
	"strings"
	"sync"
	"time"
)

var WorkloadGVRMap = map[string]*schema.GroupVersionResource{
	"Deployment":  DeploymentGVR,
	"StatefulSet": StatefulSetGVR,
	"DaemonSet":   DaemonSetGVR,
	"Job":         JobGVR,
	"CronJob":     CronJobGVR,
}
var WorkloadKinds = []string{"Deployment", "StatefulSet", "DaemonSet", "Job"}

type Helm struct {
	*ospserver.OspServer
	*kubernetes.KubeClient
	watch *WatchResource
	*DynamicResource
	//genericclioptions.RESTClientGetter
	pod *Pod
}

func newConfigFlags(kubeClient *kubernetes.KubeClient, namespace string) *genericclioptions.ConfigFlags {
	var impersonateGroup []string
	insecure := true

	// CertFile and KeyFile must be nil for the BearerToken to be used for authentication and authorization instead of the pod's service account.
	return &genericclioptions.ConfigFlags{
		Insecure:   &insecure,
		KubeConfig: &kubeClient.KubeConfigFile,
		Namespace:  utils.StringPtr(namespace),
		APIServer:  utils.StringPtr(kubeClient.Config.Host),
		//CAFile:           utils.StringPtr(kubeClient.Config.CAFile),
		BearerToken:      utils.StringPtr(kubeClient.Config.BearerToken),
		ImpersonateGroup: &impersonateGroup,
	}
}

func NewHelm(pod *Pod, kubeClient *kubernetes.KubeClient, watch *WatchResource, ospServer *ospserver.OspServer) *Helm {
	h := &Helm{
		OspServer:       ospServer,
		KubeClient:      kubeClient,
		watch:           watch,
		DynamicResource: NewDynamicResource(kubeClient, nil),
		//RESTClientGetter: newConfigFlags(kubeClient),
		pod: pod,
	}
	return h
}

type HelmQueryParams struct {
	Name   string `json:"name"`
	Output string `json:"output"`
}

func (h *Helm) buildRelease(release *release.Release) map[string]interface{} {
	return map[string]interface{}{
		"name":          release.Name,
		"namespace":     release.Namespace,
		"version":       release.Version,
		"status":        release.Info.Status,
		"chart_name":    release.Chart.Name() + "-" + release.Chart.Metadata.Version,
		"chart_version": release.Chart.Metadata.Version,
		"app_version":   release.Chart.AppVersion(),
		"last_deployed": release.Info.LastDeployed,
	}
}

func (h *Helm) List(requestParams interface{}) *utils.Response {
	actionConfig := new(action.Configuration)
	clientGetter := newConfigFlags(h.KubeClient, "")
	if err := actionConfig.Init(clientGetter, "", "", klog.Infof); err != nil {
		klog.Errorf("%+v", err)
		return &utils.Response{Code: code.ListError, Msg: err.Error()}
	}
	client := action.NewList(actionConfig)
	results, err := client.Run()
	if err != nil {
		klog.Errorf("list helm error: %s", err)
		return &utils.Response{Code: code.ListError, Msg: err.Error()}
	}
	var res []map[string]interface{}
	for _, r := range results {
		res = append(res, h.buildRelease(r))
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: res}
}

type HelmGetParams struct {
	ReleaseName   string `json:"release_name"`
	Name          string `json:"name"`
	ChartVersion  string `json:"chart_version"`
	ChartPath     string `json:"chart_path"`
	Namespace     string `json:"namespace"`
	Values        string `json:"values"`
	GetOption     string `json:"get_option"`
	WithWorkloads bool   `json:"with_workloads"`
}

func (h *Helm) Get(requestParams interface{}) *utils.Response {
	queryParams := &HelmGetParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	if queryParams.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Name is blank"}
	}
	if queryParams.Namespace == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Namespace is blank"}
	}
	actionConfig := new(action.Configuration)

	clientGetter := newConfigFlags(h.KubeClient, queryParams.Namespace)
	if err := actionConfig.Init(clientGetter, queryParams.Namespace, "", klog.Infof); err != nil {
		klog.Errorf("%+v", err)
		return &utils.Response{Code: code.ListError, Msg: err.Error()}
	}
	client := action.NewGet(actionConfig)
	releaseDetail, err := client.Run(queryParams.Name)
	if err != nil {
		klog.Errorf("get releaseDetail error: %s", err)
		return &utils.Response{Code: code.ListError, Msg: err.Error()}
	}
	var data map[string]interface{}
	status := h.GetReleaseRuntimeStatus(releaseDetail, queryParams.WithWorkloads)
	data = map[string]interface{}{
		"objects":       status.Workloads,
		"name":          releaseDetail.Name,
		"namespace":     releaseDetail.Namespace,
		"version":       releaseDetail.Version,
		"status":        status.Status,
		"chart_name":    releaseDetail.Chart.Name() + "-" + releaseDetail.Chart.Metadata.Version,
		"chart_version": releaseDetail.Chart.Metadata.Version,
		"app_version":   releaseDetail.Chart.AppVersion(),
		"last_deployed": releaseDetail.Info.LastDeployed,
	}

	return &utils.Response{Code: code.Success, Msg: "Success", Data: data}
}

func (h *Helm) Create(requestParams interface{}) *utils.Response {
	queryParams := &HelmGetParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	if queryParams.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Name is blank"}
	}
	if queryParams.Namespace == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Namespace is blank"}
	}
	if queryParams.ChartPath == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Chart path is blank"}
	}
	chart, err := h.OspServer.GetAppChart(queryParams.ChartPath)
	if err != nil {
		return &utils.Response{Code: code.ParamsError, Msg: "get chart error, " + err.Error()}
	}
	actionConfig := new(action.Configuration)

	clientGetter := newConfigFlags(h.KubeClient, queryParams.Namespace)
	if err = actionConfig.Init(clientGetter, queryParams.Namespace, "", klog.Infof); err != nil {
		klog.Errorf("init helm config error: %+v", err)
		return &utils.Response{Code: code.ApplyError, Msg: err.Error()}
	}
	actionConfig.Releases.MaxHistory = 3
	clientInstall := action.NewInstall(actionConfig)
	releaseName := queryParams.ReleaseName
	if releaseName == "" {
		releaseName = queryParams.Name
	}
	clientInstall.ReleaseName = releaseName
	clientInstall.Namespace = queryParams.Namespace
	values := make(map[string]interface{})
	err = yaml.Unmarshal([]byte(queryParams.Values), &values)
	if err != nil {
		return &utils.Response{Code: code.ParamsError, Msg: "values参数解析错误：" + err.Error()}
	}
	_, err = clientInstall.Run(chart, values)
	if err != nil {
		klog.Errorf("install release error: %s", err)
		clientUnInstall := action.NewUninstall(actionConfig)
		_, err1 := clientUnInstall.Run(releaseName)
		if err1 != nil {
			klog.Errorf("uninstall release error: %s", err)
		}
		return &utils.Response{Code: code.ApplyError, Msg: err.Error()}
	}
	return &utils.Response{Code: code.Success, Msg: "Success"}
}

func (h *Helm) Update(requestParams interface{}) *utils.Response {
	queryParams := &HelmGetParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	if queryParams.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Name is blank"}
	}
	if queryParams.Namespace == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Namespace is blank"}
	}
	chart, err := h.OspServer.GetAppChart(queryParams.ChartPath)
	if err != nil {
		return &utils.Response{Code: code.ParamsError, Msg: "get chart error, " + err.Error()}
	}

	actionConfig := new(action.Configuration)

	clientGetter := newConfigFlags(h.KubeClient, queryParams.Namespace)
	if err = actionConfig.Init(clientGetter, queryParams.Namespace, "", klog.Infof); err != nil {
		klog.Errorf("init helm config error: %+v", err)
		return &utils.Response{Code: code.ApplyError, Msg: err.Error()}
	}
	actionConfig.Releases.MaxHistory = 3

	clientInstall := action.NewUpgrade(actionConfig)
	clientInstall.Namespace = queryParams.Namespace
	values := make(map[string]interface{})
	err = yaml.Unmarshal([]byte(queryParams.Values), &values)
	if err != nil {
		return &utils.Response{Code: code.ParamsError, Msg: "values参数解析错误：" + err.Error()}
	}
	_, err = clientInstall.Run(queryParams.Name, chart, values)
	if err != nil {
		klog.Errorf("install release error: %s", err)
		return &utils.Response{Code: code.ApplyError, Msg: err.Error()}
	}
	return &utils.Response{Code: code.Success, Msg: "Success"}
}

func (h *Helm) Delete(requestParams interface{}) *utils.Response {
	queryParams := &HelmGetParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	if queryParams.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Name is blank"}
	}
	if queryParams.Namespace == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Namespace is blank"}
	}
	klog.Info(queryParams)

	actionConfig := new(action.Configuration)

	clientGetter := newConfigFlags(h.KubeClient, queryParams.Namespace)
	if err := actionConfig.Init(clientGetter, queryParams.Namespace, "", klog.Infof); err != nil {
		klog.Errorf("init helm config error: %+v", err)
		return &utils.Response{Code: code.ApplyError, Msg: err.Error()}
	}
	clientInstall := action.NewUninstall(actionConfig)
	_, err := clientInstall.Run(queryParams.Name)
	if err != nil {
		klog.Errorf("uninstall release error: %s", err)
		return &utils.Response{Code: code.ApplyError, Msg: err.Error()}
	}
	return &utils.Response{Code: code.Success, Msg: "Success"}
}

func (h *Helm) GetValues(requestParams interface{}) *utils.Response {
	queryParams := &HelmGetParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	if queryParams.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Name is blank"}
	}
	if queryParams.Namespace == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Namespace is blank"}
	}
	actionConfig := new(action.Configuration)

	clientGetter := newConfigFlags(h.KubeClient, queryParams.Namespace)
	if err := actionConfig.Init(clientGetter, queryParams.Namespace, "", klog.Infof); err != nil {
		klog.Errorf("%+v", err)
		return &utils.Response{Code: code.ListError, Msg: err.Error()}
	}
	client := action.NewGet(actionConfig)
	res, err := client.Run(queryParams.Name)
	if err != nil {
		klog.Errorf("get release error: %s", err)
		return &utils.Response{Code: code.ListError, Msg: err.Error()}
	}
	var values string
	for _, f := range res.Chart.Raw {
		if f.Name == "values.yaml" {
			values = string(f.Data)
			break
		}
	}
	data := map[string]interface{}{
		"config": res.Config,
		"values": values,
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: data}
}

type RuntimeStatusParams struct {
	Namespace     string   `json:"namespace"`
	Names         []string `json:"names"`
	WithWorkloads bool     `json:"with_workloads"`
}

const (
	ReleaseStatusRunning      = "Running"
	ReleaseStatusNotReady     = "NotReady"
	ReleaseStatusRunningFault = "RunningFault"
)

type ReleaseRuntimeStatus struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Workloads []*unstructured.Unstructured
}

func (h *Helm) Status(reqParams interface{}) *utils.Response {
	params := &RuntimeStatusParams{}
	json.Unmarshal(reqParams.([]byte), params)
	if params.Namespace == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Namespace is blank"}
	}
	actionConfig := new(action.Configuration)

	clientGetter := newConfigFlags(h.KubeClient, params.Namespace)
	if err := actionConfig.Init(clientGetter, params.Namespace, "", klog.Infof); err != nil {
		klog.Errorf("%+v", err)
		return &utils.Response{Code: code.ListError, Msg: err.Error()}
	}
	client := action.NewList(actionConfig)
	releases, err := client.Run()
	if err != nil {
		klog.Errorf("list helm error: %s", err)
		return &utils.Response{Code: code.ListError, Msg: err.Error()}
	}
	var res []*ReleaseRuntimeStatus

	wg := &sync.WaitGroup{}

	for _, releaseDetail := range releases {
		if len(params.Names) == 0 || utils.Contains(params.Names, releaseDetail.Name) {
			wg.Add(1)
			go func(releaseDetail *release.Release) {
				defer wg.Done()
				status := h.GetReleaseRuntimeStatus(releaseDetail, params.WithWorkloads)
				res = append(res, status)
			}(releaseDetail)
		}
	}
	wg.Wait()
	return &utils.Response{Code: code.Success, Data: res}
}

func (h *Helm) GetReleaseObjects(release *release.Release) []*unstructured.Unstructured {
	var objects []*unstructured.Unstructured
	yamlList := strings.SplitAfter(release.Manifest, "\n---")
	for _, objectYaml := range yamlList {
		unstructuredObj := &unstructured.Unstructured{}
		yamlBytes := []byte(objectYaml)
		decoder := yamlutil.NewYAMLOrJSONDecoder(bytes.NewBuffer(yamlBytes), len(yamlBytes))
		if err := decoder.Decode(unstructuredObj); err != nil {
			klog.Error("decode k8sObject objectYaml: ", objectYaml)
			klog.Error("decode k8sObject error: ", err)
			continue
		} else {
			objects = append(objects, unstructuredObj)
		}
	}
	return objects
}

func (h *Helm) GetReleaseRuntimeStatus(release *release.Release, withWorkloads bool) *ReleaseRuntimeStatus {
	var workloads []*unstructured.Unstructured
	status := &ReleaseRuntimeStatus{
		Name:      release.Name,
		Status:    ReleaseStatusRunning,
		Workloads: workloads,
	}
	//isAllReady := true
	objects := h.GetReleaseObjects(release)
	namespace := release.Namespace
	for i, object := range objects {
		if utils.Contains(WorkloadKinds, object.GetKind()) {
			obj, err := h.DynamicClient.Resource(*WorkloadGVRMap[object.GetKind()]).Namespace(namespace).Get(h.context, object.GetName(), metav1.GetOptions{})
			if err != nil {
				klog.Errorf("get namespace %s workload %s/%s error: %s", namespace, object.GetKind(), object.GetName(), err.Error())
				if withWorkloads {
					workloads = append(workloads, object)
				}
				continue
			}
			if withWorkloads {
				workloads = append(workloads, obj)
			}
			podLabels, ok, err := unstructured.NestedStringMap(obj.Object, "spec", "selector", "matchLabels")
			if err != nil {
				klog.Errorf("get namespace %s workload %s/%s labels error: %s", namespace, object.GetKind(), object.GetName(), err.Error())
				if withWorkloads {
					workloads = append(workloads, obj)
				}
				continue
			}
			if !ok {
				klog.Errorf("get namespace %s workload %s/%s labels error", namespace, object.GetKind(), object.GetName())
				if withWorkloads {
					workloads = append(workloads, obj)
				}
				continue
			}
			pods, err := h.DynamicClient.Resource(*PodGVR).Namespace(namespace).List(h.context, metav1.ListOptions{
				LabelSelector: labels.Set(podLabels).AsSelector().String(),
			})
			if err != nil {
				klog.Errorf("get namespace %s workload %s/%s pods error: %s", namespace, object.GetKind(), object.GetName(), err.Error())
			}
			if pods != nil {
				for idx, _ := range pods.Items {
					if withWorkloads {
						workloads = append(workloads, &pods.Items[idx])
					}
					var p v1.Pod
					err = runtime.DefaultUnstructuredConverter.FromUnstructured(pods.Items[idx].Object, &p)
					if err != nil {
						klog.Errorf("convert to pod error: %s", err.Error())
					} else {
						status.Status = h.GetPodStatus(status.Status, &p)
					}
				}
			}
		} else if object.GetKind() == "Pod" {
			pod, err := h.DynamicClient.Resource(*PodGVR).Namespace(namespace).Get(h.context, object.GetName(), metav1.GetOptions{})
			if err != nil {
				klog.Warning("get release pods error: ", err)
			} else {
				if withWorkloads {
					workloads = append(workloads, pod)
				}
				var p v1.Pod
				err = runtime.DefaultUnstructuredConverter.FromUnstructured(pod.Object, &p)
				if err != nil {
					klog.Errorf("convert to pod error: %s", err.Error())
				} else {
					status.Status = h.GetPodStatus(status.Status, &p)
				}
			}
		} else if object.GetKind() == "Service" {
			svc, err := h.DynamicClient.Resource(*ServiceGVR).Namespace(namespace).Get(h.context, object.GetName(), metav1.GetOptions{})
			if err != nil {
				klog.Warning("get release service error: ", err)
			} else {
				if withWorkloads {
					workloads = append(workloads, svc)
				}
			}
		} else {
			if withWorkloads {
				workloads = append(workloads, objects[i])
			}
		}
	}
	status.Workloads = workloads
	//if isAllReady {
	//	return status
	//}
	//secondDuration := time.Now().Sub(release.Info.LastDeployed.Time).Seconds()
	//if secondDuration > 600 {
	//	status.Status = ReleaseStatusRunningFault
	//} else {
	//	status.Status = ReleaseStatusNotReady
	//}
	return status
}

func (h *Helm) GetPodStatus(status string, pod *v1.Pod) string {
	if status == ReleaseStatusRunningFault {
		return status
	}
	podReady := h.pod.IsPodReady(pod)
	if podReady {
		return status
	}
	secondDuration := time.Now().Sub(pod.CreationTimestamp.Time).Seconds()
	klog.Infof("%d", secondDuration)
	if secondDuration > 600 {
		return ReleaseStatusRunningFault
	} else {
		return ReleaseStatusNotReady
	}
}

func (h *Helm) GetPodsReady(namespace string, labelsMap map[string]string) (bool, *v1.PodList) {
	podList, err := h.GetPodList(namespace, labelsMap)
	if err != nil {
		klog.Warning("get release pods error: ", err)
		return false, nil
	}
	for _, pod := range podList.Items {
		if isReady := h.pod.IsPodReady(&pod); !isReady {
			return false, nil
		}
	}
	return true, podList
}

func (h *Helm) GetPodList(namespace string, labelsMap map[string]string) (*v1.PodList, error) {
	labelSelector := metav1.LabelSelector{MatchLabels: labelsMap}
	return h.ClientSet.CoreV1().Pods(namespace).List(h.context, metav1.ListOptions{
		LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
	})
}

func (h *Helm) GetReleaseRuntimeObjects(release *release.Release) []*unstructured.Unstructured {
	var workloads []*unstructured.Unstructured
	objects := h.GetReleaseObjects(release)
	namespace := release.Namespace
	for i, object := range objects {
		if utils.Contains(WorkloadKinds, object.GetKind()) {
			obj, err := h.DynamicClient.Resource(*WorkloadGVRMap[object.GetKind()]).Namespace(namespace).Get(h.context, object.GetName(), metav1.GetOptions{})
			if err != nil {
				klog.Errorf("get namespace %s workload %s/%s error: %s", namespace, object.GetKind(), object.GetName(), err.Error())
				workloads = append(workloads, object)
				continue
			}
			workloads = append(workloads, obj)
			podLabels, ok, err := unstructured.NestedStringMap(obj.Object, "spec", "selector", "matchLabels")
			if err != nil {
				klog.Errorf("get namespace %s workload %s/%s labels error: %s", namespace, object.GetKind(), object.GetName(), err.Error())
				workloads = append(workloads, obj)
				continue
			}
			if !ok {
				klog.Errorf("get namespace %s workload %s/%s labels error", namespace, object.GetKind(), object.GetName())
				workloads = append(workloads, obj)
				continue
			}
			pods, err := h.DynamicClient.Resource(*PodGVR).Namespace(namespace).List(h.context, metav1.ListOptions{
				LabelSelector: labels.Set(podLabels).AsSelector().String(),
			})
			if err != nil {
				klog.Errorf("get namespace %s workload %s/%s pods error: %s", namespace, object.GetKind(), object.GetName(), err.Error())
			}
			if pods != nil {
				for idx, _ := range pods.Items {
					workloads = append(workloads, &pods.Items[idx])
				}
			}
		} else if object.GetKind() == "Pod" {
			pod, err := h.DynamicClient.Resource(*PodGVR).Namespace(namespace).Get(h.context, object.GetName(), metav1.GetOptions{})
			if err != nil {
				klog.Warning("get release pods error: ", err)
			} else {
				workloads = append(workloads, pod)
			}
		} else if object.GetKind() == "Service" {
			svc, err := h.DynamicClient.Resource(*ServiceGVR).Namespace(namespace).Get(h.context, object.GetName(), metav1.GetOptions{})
			if err != nil {
				klog.Warning("get release service error: ", err)
			} else {
				workloads = append(workloads, svc)
			}
		} else {
			workloads = append(workloads, objects[i])
		}
	}
	return workloads
}
