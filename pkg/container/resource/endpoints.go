package resource

import (
	"encoding/json"
	"fmt"
	"github.com/openspacee/ospagent/pkg/kubernetes"
	"github.com/openspacee/ospagent/pkg/utils"
	"github.com/openspacee/ospagent/pkg/utils/code"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog"
)

type Endpoints struct {
	watch *WatchResource
	*DynamicResource
}

func NewEndpoints(kubeClient *kubernetes.KubeClient, watch *WatchResource) *Endpoints {
	s := &Endpoints{
		watch: watch,
		DynamicResource: NewDynamicResource(kubeClient, &schema.GroupVersionResource{
			Group:    "",
			Version:  "v1",
			Resource: "endpoints",
		}),
	}
	//s.DoWatch()
	return s
}

func (e *Endpoints) DoWatch() {
	informer := e.KubeClient.EndpointsInformer().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    e.watch.WatchAdd(utils.WatchEndpoints),
		UpdateFunc: e.watch.WatchUpdate(utils.WatchEndpoints),
		DeleteFunc: e.watch.WatchDelete(utils.WatchEndpoints),
	})
}

type BuildEndpoints struct {
	UID             string                  `json:"uid"`
	Name            string                  `json:"name"`
	Namespace       string                  `json:"namespace"`
	Subsets         []corev1.EndpointSubset `json:"subsets"`
	Created         metav1.Time             `json:"created"`
	ResourceVersion string                  `json:"resource_version"`
}

func (e *Endpoints) ToBuildEndpoints(endpoints *corev1.Endpoints) *BuildEndpoints {
	if endpoints == nil {
		return nil
	}
	data := &BuildEndpoints{
		UID:             string(endpoints.UID),
		Name:            endpoints.Name,
		Namespace:       endpoints.Namespace,
		Subsets:         endpoints.Subsets,
		Created:         endpoints.CreationTimestamp,
		ResourceVersion: endpoints.ResourceVersion,
	}

	return data
}

type EndpointsQueryParams struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	UID       string `json:"uid"`
	Output    string `json:"output"`
}

type EndpointsUpdateParams struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Replicas  int32  `json:"replicas"`
}

func (e *Endpoints) List(requestParams interface{}) *utils.Response {
	queryParams := &EndpointsQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	list, err := e.KubeClient.InformerRegistry.EndpointsInformer().Lister().List(labels.Everything())
	if err != nil {
		return &utils.Response{
			Code: code.ListError,
			Msg:  err.Error(),
		}
	}
	var endpoints []*BuildEndpoints
	for _, ds := range list {
		if queryParams.UID != "" && string(ds.UID) != queryParams.UID {
			continue
		}
		if queryParams.Namespace != "" && ds.Namespace != queryParams.Namespace {
			continue
		}
		if queryParams.Name != "" && ds.Name != queryParams.Name {
			continue
		}
		endpoints = append(endpoints, e.ToBuildEndpoints(ds))
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: endpoints}
}

func (e *Endpoints) Get(requestParams interface{}) *utils.Response {
	queryParams := &EndpointsQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	if queryParams.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Endpoints name is blank"}
	}
	if queryParams.Namespace == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Namespace is blank"}
	}
	endpoints, err := e.KubeClient.InformerRegistry.EndpointsInformer().Lister().Endpoints(queryParams.Namespace).Get(queryParams.Name)
	if err != nil {
		return &utils.Response{Code: code.GetError, Msg: err.Error()}
	}
	if queryParams.Output == "yaml" {
		const mediaType = runtime.ContentTypeYAML
		rscheme := runtime.NewScheme()
		corev1.AddToScheme(rscheme)
		codecs := serializer.NewCodecFactory(rscheme)
		info, ok := runtime.SerializerInfoForMediaType(codecs.SupportedMediaTypes(), mediaType)
		if !ok {
			return &utils.Response{Code: code.Success, Msg: fmt.Sprintf("unsupported media type %q", mediaType)}
		}

		encoder := codecs.EncoderForVersion(info.Serializer, e.GroupVersion())
		//klog.Info(a)
		d, e := runtime.Encode(encoder, endpoints)
		if e != nil {
			klog.Error(e)
			return &utils.Response{Code: code.Success, Msg: e.Error()}
		}
		return &utils.Response{Code: code.Success, Msg: "Success", Data: string(d)}
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: endpoints}
}

func (e *Endpoints) UpdateObj(updateParams interface{}) *utils.Response {
	params := &EndpointsUpdateParams{}
	json.Unmarshal(updateParams.([]byte), params)
	if params.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Endpoints name is blank"}
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
		result, getErr := e.KubeClient.InformerRegistry.EndpointsInformer().Lister().Endpoints(params.Namespace).Get(params.Name)
		if getErr != nil {
			panic(fmt.Errorf("failed to get latest version of Endpoints: %v", getErr))
		}

		//result.Spec.Replicas = &params.Replicas
		_, updateErr := e.ClientSet.CoreV1().Endpoints(params.Namespace).Update(e.context, result, metav1.UpdateOptions{})
		return updateErr
	})
	if retryErr != nil {
		klog.Errorf("Update failed: %v", retryErr)
		return &utils.Response{Code: code.ParamsError, Msg: retryErr.Error()}
	}
	return &utils.Response{Code: code.Success, Msg: "Success"}
}
