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

type DaemonSet struct {
	watch *WatchResource
	*DynamicResource
}

func NewDaemonSet(kubeClient *kubernetes.KubeClient, watch *WatchResource) *DaemonSet {
	d := &DaemonSet{
		watch: watch,
		DynamicResource: NewDynamicResource(kubeClient, &schema.GroupVersionResource{
			Group:    "apps",
			Version:  "v1",
			Resource: "daemonsets",
		}),
	}
	d.DoWatch()
	return d
}

func (d *DaemonSet) DoWatch() {
	informer := d.KubeClient.DaemonSetInformer().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    d.watch.WatchAdd(utils.WatchDaemonset),
		UpdateFunc: d.watch.WatchUpdate(utils.WatchDaemonset),
		DeleteFunc: d.watch.WatchDelete(utils.WatchDaemonset),
	})
}

type BuildDaemonSet struct {
	UID                    string            `json:"uid"`
	Name                   string            `json:"name"`
	Namespace              string            `json:"namespace"`
	DesiredNumberScheduled int32             `json:"desired_number_scheduled"`
	NumberReady            int32             `json:"number_ready"`
	ResourceVersion        string            `json:"resource_version"`
	Strategy               string            `json:"strategy"`
	Conditions             []string          `json:"conditions"`
	NodeSelector           map[string]string `json:"node_selector"`
	Created                metav1.Time       `json:"created"`
}

func (d *DaemonSet) ToBuildDaemonSet(ds *v1.DaemonSet) *BuildDaemonSet {
	if ds == nil {
		return nil
	}
	var conditions []string
	for _, c := range ds.Status.Conditions {
		if c.Status == corev1.ConditionTrue {
			conditions = append(conditions, string(c.Type))
		}
	}
	data := &BuildDaemonSet{
		UID:                    string(ds.UID),
		Name:                   ds.Name,
		Namespace:              ds.Namespace,
		DesiredNumberScheduled: ds.Status.DesiredNumberScheduled,
		NumberReady:            ds.Status.NumberReady,
		ResourceVersion:        ds.ResourceVersion,
		Conditions:             conditions,
		Strategy:               string(ds.Spec.UpdateStrategy.Type),
		NodeSelector:           ds.Spec.Template.Spec.NodeSelector,
		Created:                ds.CreationTimestamp,
	}

	return data
}

type DaemonSetQueryParams struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	UID       string `json:"uid"`
	Output    string `json:"output"`
}

type DaemonSetUpdateParams struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Replicas  int32  `json:"replicas"`
}

func (d *DaemonSet) List(requestParams interface{}) *utils.Response {
	queryParams := &StatefulSetQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	list, err := d.KubeClient.InformerRegistry.DaemonSetInformer().Lister().List(labels.Everything())
	if err != nil {
		return &utils.Response{
			Code: code.ListError,
			Msg:  err.Error(),
		}
	}
	var dss []*BuildDaemonSet
	for _, ds := range list {
		if queryParams.UID != "" && string(ds.UID) != queryParams.UID {
			continue
		}
		if queryParams.Namespace != "" && ds.Namespace != queryParams.Namespace {
			continue
		}
		if queryParams.Name != "" && strings.Contains(ds.Name, queryParams.Name) {
			continue
		}
		dss = append(dss, d.ToBuildDaemonSet(ds))
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: dss}
}

func (d *DaemonSet) Get(requestParams interface{}) *utils.Response {
	queryParams := &StatefulSetQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	if queryParams.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "DaemonSet name is blank"}
	}
	if queryParams.Namespace == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Namespace is blank"}
	}
	ds, err := d.KubeClient.InformerRegistry.DaemonSetInformer().Lister().DaemonSets(queryParams.Namespace).Get(queryParams.Name)
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
		d, e := runtime.Encode(encoder, ds)
		if e != nil {
			klog.Error(e)
			return &utils.Response{Code: code.Success, Msg: e.Error()}
		}
		return &utils.Response{Code: code.Success, Msg: "Success", Data: string(d)}
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: ds}
}

func (d *DaemonSet) UpdateObj(updateParams interface{}) *utils.Response {
	params := &StatefulSetUpdateParams{}
	json.Unmarshal(updateParams.([]byte), params)
	if params.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "DaemonSet name is blank"}
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
		result, getErr := d.KubeClient.InformerRegistry.DaemonSetInformer().Lister().DaemonSets(params.Namespace).Get(params.Name)
		if getErr != nil {
			panic(fmt.Errorf("failed to get latest version of DaemonSet: %v", getErr))
		}

		//result.Spec.Replicas = &params.Replicas
		_, updateErr := d.ClientSet.AppsV1().DaemonSets(params.Namespace).Update(d.context, result, metav1.UpdateOptions{})
		return updateErr
	})
	if retryErr != nil {
		klog.Errorf("Update failed: %v", retryErr)
		return &utils.Response{Code: code.ParamsError, Msg: retryErr.Error()}
	}
	return &utils.Response{Code: code.Success, Msg: "Success"}
}
