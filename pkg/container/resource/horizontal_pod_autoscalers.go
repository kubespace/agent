package resource

import (
	"encoding/json"
	"fmt"
	"github.com/openspacee/ospagent/pkg/kubernetes"
	"github.com/openspacee/ospagent/pkg/utils"
	"github.com/openspacee/ospagent/pkg/utils/code"
	"github.com/openspacee/ospagent/pkg/websocket"
	hpav2beta1 "k8s.io/api/autoscaling/v2beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/klog"
)

type HorizontalPodAutoscaler struct {
	*kubernetes.KubeClient
	websocket.SendResponse
	*DynamicResource
}

type BuildHorizontalPodAutoscaler struct {
	Name          string                                   `json:"name"`
	NameSpace     string                                   `json:"namespace"`
	MinPods       *int32                                   `json:"min_pods"`
	MaxPods       int32                                    `json:"max_pods"`
	Replicas      int32                                    `json:"replicas"`
	Status        hpav2beta1.HorizontalPodAutoscalerStatus `json:"status"`
	CreateTime    metav1.Time                              `json:"create_time"`
	TargetCpuPer  *int32                                   `json:"target_cpu_per"`
	CurrentCpuPer *int32                                   `json:"current_cpu_per"`
}

type HorizontalPodAutoscalerQueryParams struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Output    string `json:"output"`
}

func (h *HorizontalPodAutoscaler) ToBuildHorizontalPodAutoscaler(hpa *hpav2beta1.HorizontalPodAutoscaler) *BuildHorizontalPodAutoscaler {
	if hpa == nil {
		return nil
	}

	hpaData := &BuildHorizontalPodAutoscaler{
		Name:      hpa.Name,
		NameSpace: hpa.Namespace,
		MinPods:   hpa.Spec.MinReplicas,
		MaxPods:   hpa.Spec.MaxReplicas,
		Replicas:  hpa.Status.CurrentReplicas,
		Status:    hpa.Status,
		//CreateTime: fmt.Sprint(hpa.CreationTimestamp),
		CreateTime: hpa.CreationTimestamp,
		//TargetCpuPer: 	hpa.Spec.TargetCPUUtilizationPercentage,
		//CurrentCpuPer:	hpa.Status.CurrentCPUUtilizationPercentage,
	}

	return hpaData
}

func NewHorizontalPodAutoscaler(kubeClient *kubernetes.KubeClient, sendResponse websocket.SendResponse) *HorizontalPodAutoscaler {
	return &HorizontalPodAutoscaler{
		KubeClient:   kubeClient,
		SendResponse: sendResponse,
		DynamicResource: NewDynamicResource(kubeClient, &schema.GroupVersionResource{
			Group:    "autoscaling",
			Version:  "v2beta1",
			Resource: "horizontalpodautoscalers",
		}),
	}
}

func (h *HorizontalPodAutoscaler) List(requestParams interface{}) *utils.Response {
	HpaList, err := h.KubeClient.InformerRegistry.HorizontalPodAutoscalerInformer().Lister().List(labels.Everything())
	if err != nil {
		return &utils.Response{
			Code: code.ListError,
			Msg:  err.Error(),
		}
	}
	var HpaResource []*BuildHorizontalPodAutoscaler
	for _, hp := range HpaList {
		HpaResource = append(HpaResource, h.ToBuildHorizontalPodAutoscaler(hp))
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: HpaResource}
}

func (h *HorizontalPodAutoscaler) Get(requestParams interface{}) *utils.Response {
	queryParams := &HorizontalPodAutoscalerQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	if queryParams.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Name is blank"}
	}
	if queryParams.Namespace == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Namespace is blank"}
	}
	Hpa, err := h.KubeClient.InformerRegistry.HorizontalPodAutoscalerInformer().Lister().HorizontalPodAutoscalers(queryParams.Namespace).Get(queryParams.Name)
	if err != nil {
		return &utils.Response{Code: code.GetError, Msg: err.Error()}
	}
	if queryParams.Output == "yaml" {
		const mediaType = runtime.ContentTypeYAML
		rscheme := runtime.NewScheme()
		hpav2beta1.AddToScheme(rscheme)
		codecs := serializer.NewCodecFactory(rscheme)
		info, ok := runtime.SerializerInfoForMediaType(codecs.SupportedMediaTypes(), mediaType)
		if !ok {
			return &utils.Response{Code: code.Success, Msg: fmt.Sprintf("unsupported media type %q", mediaType)}
		}

		encoder := codecs.EncoderForVersion(info.Serializer, h.GroupVersion())
		d, e := runtime.Encode(encoder, Hpa)
		if e != nil {
			klog.Error(e)
			return &utils.Response{Code: code.EncodeError, Msg: e.Error()}
		}
		klog.Info(d)
		return &utils.Response{Code: code.Success, Msg: "Success", Data: string(d)}
	}

	return &utils.Response{Code: code.Success, Msg: "Success", Data: Hpa}
}
