package resource

import (
	"encoding/json"
	"fmt"
	"github.com/kubespace/agent/pkg/kubernetes"
	"github.com/kubespace/agent/pkg/utils"
	"github.com/kubespace/agent/pkg/utils/code"
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

var ServiceGVR = &schema.GroupVersionResource{
	Group:    "",
	Version:  "v1",
	Resource: "services",
}

type Service struct {
	watch *WatchResource
	*DynamicResource
}

func NewService(kubeClient *kubernetes.KubeClient, watch *WatchResource) *Service {
	s := &Service{
		watch:           watch,
		DynamicResource: NewDynamicResource(kubeClient, ServiceGVR),
	}
	s.DoWatch()
	return s
}

func (s *Service) DoWatch() {
	informer := s.KubeClient.ServiceInformer().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    s.watch.WatchAdd(utils.WatchService),
		UpdateFunc: s.watch.WatchUpdate(utils.WatchService),
		DeleteFunc: s.watch.WatchDelete(utils.WatchService),
	})
}

type BuildService struct {
	UID             string               `json:"uid"`
	Name            string               `json:"name"`
	Namespace       string               `json:"namespace"`
	Type            string               `json:"type"`
	ClusterIP       string               `json:"cluster_ip"`
	Ports           []corev1.ServicePort `json:"ports"`
	ExternalIP      []string             `json:"external_ip"`
	Selector        map[string]string    `json:"selector"`
	ResourceVersion string               `json:"resource_version"`
	Created         metav1.Time          `json:"created"`
}

func (s *Service) ToBuildService(service *corev1.Service) *BuildService {
	if service == nil {
		return nil
	}
	data := &BuildService{
		UID:             string(service.UID),
		Name:            service.Name,
		Namespace:       service.Namespace,
		Type:            string(service.Spec.Type),
		ClusterIP:       string(service.Spec.ClusterIP),
		Ports:           service.Spec.Ports,
		ExternalIP:      service.Spec.ExternalIPs,
		Selector:        service.Spec.Selector,
		Created:         service.CreationTimestamp,
		ResourceVersion: service.ResourceVersion,
	}

	return data
}

type ServiceQueryParams struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	UID       string            `json:"uid"`
	Output    string            `json:"output"`
	Labels    map[string]string `json:"labels"`
}

type ServiceUpdateParams struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Replicas  int32  `json:"replicas"`
}

func (s *Service) List(requestParams interface{}) *utils.Response {
	queryParams := &ServiceQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	var selector labels.Selector
	if queryParams.Labels != nil {
		selector = labels.Set(queryParams.Labels).AsSelector()
	} else {
		selector = labels.Everything()
	}
	list, err := s.KubeClient.ServiceInformer().Lister().Services(queryParams.Namespace).List(selector)
	if err != nil {
		return &utils.Response{
			Code: code.ListError,
			Msg:  err.Error(),
		}
	}
	var services []*BuildService
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
		services = append(services, s.ToBuildService(ds))
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: services}
}

func (s *Service) Get(requestParams interface{}) *utils.Response {
	queryParams := &ServiceQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	if queryParams.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Service name is blank"}
	}
	if queryParams.Namespace == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Namespace is blank"}
	}
	service, err := s.KubeClient.InformerRegistry.ServiceInformer().Lister().Services(queryParams.Namespace).Get(queryParams.Name)
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

		encoder := codecs.EncoderForVersion(info.Serializer, s.GroupVersion())
		//klog.Info(a)
		d, e := runtime.Encode(encoder, service)
		if e != nil {
			klog.Error(e)
			return &utils.Response{Code: code.Success, Msg: e.Error()}
		}
		return &utils.Response{Code: code.Success, Msg: "Success", Data: string(d)}
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: service}
}

func (s *Service) UpdateObj(updateParams interface{}) *utils.Response {
	params := &ServiceUpdateParams{}
	json.Unmarshal(updateParams.([]byte), params)
	if params.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Service name is blank"}
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
		result, getErr := s.KubeClient.InformerRegistry.ServiceInformer().Lister().Services(params.Namespace).Get(params.Name)
		if getErr != nil {
			panic(fmt.Errorf("failed to get latest version of Service: %v", getErr))
		}

		//result.Spec.Replicas = &params.Replicas
		_, updateErr := s.ClientSet.CoreV1().Services(params.Namespace).Update(s.context, result, metav1.UpdateOptions{})
		return updateErr
	})
	if retryErr != nil {
		klog.Errorf("Update failed: %v", retryErr)
		return &utils.Response{Code: code.ParamsError, Msg: retryErr.Error()}
	}
	return &utils.Response{Code: code.Success, Msg: "Success"}
}
