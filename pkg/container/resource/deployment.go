package resource

import (
	"encoding/json"
	"fmt"
	"github.com/kubespace/agent/pkg/kubernetes"
	"github.com/kubespace/agent/pkg/utils"
	"github.com/kubespace/agent/pkg/utils/code"
	"k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog"
	"strings"
)

var DeploymentGVR = &schema.GroupVersionResource{
	Group:    "apps",
	Version:  "v1",
	Resource: "deployments",
}

type Deployment struct {
	watch *WatchResource
	*DynamicResource
}

func NewDeployment(kubeClient *kubernetes.KubeClient, watch *WatchResource) *Deployment {
	d := &Deployment{
		watch:           watch,
		DynamicResource: NewDynamicResource(kubeClient, DeploymentGVR),
	}
	d.DoWatch()
	return d
}

func (d *Deployment) DoWatch() {
	dpInformer := d.KubeClient.DeploymentInformer().Informer()
	dpInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    d.watch.WatchAdd(utils.WatchDeployment),
		UpdateFunc: d.watch.WatchUpdate(utils.WatchDeployment),
		DeleteFunc: d.watch.WatchDelete(utils.WatchDeployment),
	})
}

type BuildDeployment struct {
	UID                 string      `json:"uid"`
	Name                string      `json:"name"`
	Namespace           string      `json:"namespace"`
	Replicas            int32       `json:"replicas"`
	StatusReplicas      int32       `json:"status_replicas"`
	ReadyReplicas       int32       `json:"ready_replicas"`
	UpdatedReplicas     int32       `json:"updated_replicas"`
	UnavailableReplicas int32       `json:"unavailable_replicas"`
	AvailableReplicas   int32       `json:"available_replicas"`
	ResourceVersion     string      `json:"resource_version"`
	Strategy            string      `json:"strategy"`
	Conditions          []string    `json:"conditions"`
	Created             metav1.Time `json:"created"`
}

func (d *Deployment) ToBuildDeployment(dp *v1.Deployment) *BuildDeployment {
	if dp == nil {
		return nil
	}
	var conditions []string
	for _, c := range dp.Status.Conditions {
		if c.Status == corev1.ConditionTrue {
			conditions = append(conditions, string(c.Type))
		}
	}
	dpData := &BuildDeployment{
		UID:                 string(dp.UID),
		Name:                dp.Name,
		Namespace:           dp.Namespace,
		Replicas:            *dp.Spec.Replicas,
		StatusReplicas:      dp.Status.Replicas,
		ReadyReplicas:       dp.Status.ReadyReplicas,
		UpdatedReplicas:     dp.Status.UpdatedReplicas,
		UnavailableReplicas: dp.Status.UnavailableReplicas,
		AvailableReplicas:   dp.Status.AvailableReplicas,
		ResourceVersion:     dp.ResourceVersion,
		Strategy:            string(dp.Spec.Strategy.Type),
		Conditions:          conditions,
		Created:             dp.CreationTimestamp,
	}

	return dpData
}

type DeploymentQueryParams struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	UID       string `json:"uid"`
	Output    string `json:"output"`
}

type DeploymentUpdateParams struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Replicas  int32  `json:"replicas"`
}

func (d *Deployment) List(requestParams interface{}) *utils.Response {
	queryParams := &DeploymentQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	dpList, err := d.KubeClient.InformerRegistry.DeploymentInformer().Lister().List(labels.Everything())
	if err != nil {
		return &utils.Response{
			Code: code.ListError,
			Msg:  err.Error(),
		}
	}
	var dps []*BuildDeployment
	for _, dp := range dpList {
		if queryParams.UID != "" && string(dp.UID) != queryParams.UID {
			continue
		}
		if queryParams.Namespace != "" && dp.Namespace != queryParams.Namespace {
			continue
		}
		if queryParams.Name != "" && strings.Contains(dp.Name, queryParams.Name) {
			continue
		}
		dps = append(dps, d.ToBuildDeployment(dp))
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: dps}
}

func (d *Deployment) Get(requestParams interface{}) *utils.Response {
	queryParams := &DeploymentQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	if queryParams.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Deployment name is blank"}
	}
	if queryParams.Namespace == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Namespace is blank"}
	}
	dp, err := d.KubeClient.InformerRegistry.DeploymentInformer().Lister().Deployments(queryParams.Namespace).Get(queryParams.Name)
	if err != nil {
		return &utils.Response{Code: code.GetError, Msg: err.Error()}
	}
	if queryParams.Output == "yaml" {
		const mediaType = runtime.ContentTypeYAML
		rscheme := runtime.NewScheme()
		v1.AddToScheme(rscheme)
		codecs := serializer.NewCodecFactory(rscheme)
		info, ok := runtime.SerializerInfoForMediaType(codecs.SupportedMediaTypes(), mediaType)
		if !ok {
			return &utils.Response{Code: code.Success, Msg: fmt.Sprintf("unsupported media type %q", mediaType)}
		}

		encoder := codecs.EncoderForVersion(info.Serializer, d.GroupVersion())
		//klog.Info(a)
		d, e := runtime.Encode(encoder, dp)
		if e != nil {
			klog.Error(e)
			return &utils.Response{Code: code.Success, Msg: e.Error()}
		}
		return &utils.Response{Code: code.Success, Msg: "Success", Data: string(d)}
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: dp}
}

func (d *Deployment) UpdateObj(updateParams interface{}) *utils.Response {
	params := &DeploymentUpdateParams{}
	json.Unmarshal(updateParams.([]byte), params)
	if params.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Deployment name is blank"}
	}
	if params.Namespace == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Namespace is blank"}
	}
	if params.Replicas < 1 {
		return &utils.Response{Code: code.ParamsError, Msg: "Replicas is less than 1"}
	}
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Retrieve the latest version of Deployment before attempting update
		// RetryOnConflict uses exponential backoff to avoid exhausting the apiserver
		result, getErr := d.KubeClient.InformerRegistry.DeploymentInformer().Lister().Deployments(params.Namespace).Get(params.Name)
		if getErr != nil {
			panic(fmt.Errorf("failed to get latest version of Deployment: %v", getErr))
		}

		result.Spec.Replicas = &params.Replicas
		_, updateErr := d.ClientSet.AppsV1().Deployments(params.Namespace).Update(d.context, result, metav1.UpdateOptions{})
		return updateErr
	})
	if retryErr != nil {
		klog.Errorf("Update failed: %v", retryErr)
		return &utils.Response{Code: code.ParamsError, Msg: retryErr.Error()}
	}
	return &utils.Response{Code: code.Success, Msg: "Success"}
}
