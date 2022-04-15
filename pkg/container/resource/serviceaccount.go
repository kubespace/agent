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

type ServiceAccount struct {
	watch *WatchResource
	*DynamicResource
}

func NewServiceAccount(kubeClient *kubernetes.KubeClient, watch *WatchResource) *ServiceAccount {
	s := &ServiceAccount{
		watch: watch,
		DynamicResource: NewDynamicResource(kubeClient, &schema.GroupVersionResource{
			Group:    "",
			Version:  "v1",
			Resource: "serviceaccounts",
		}),
	}
	s.DoWatch()
	return s
}

func (s *ServiceAccount) DoWatch() {
	informer := s.KubeClient.ServiceAccountInformer().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    s.watch.WatchAdd(utils.WatchServiceAccount),
		UpdateFunc: s.watch.WatchUpdate(utils.WatchServiceAccount),
		DeleteFunc: s.watch.WatchDelete(utils.WatchServiceAccount),
	})
}

type BuildServiceAccount struct {
	UID             string                   `json:"uid"`
	Name            string                   `json:"name"`
	Namespace       string                   `json:"namespace"`
	ResourceVersion string                   `json:"resource_version"`
	Secrets         []corev1.ObjectReference `json:"secrets"`
	Created         metav1.Time              `json:"created"`
}

func (s *ServiceAccount) ToBuildServiceAccount(serviceAccount *corev1.ServiceAccount) *BuildServiceAccount {
	if serviceAccount == nil {
		return nil
	}
	data := &BuildServiceAccount{
		UID:             string(serviceAccount.UID),
		Name:            serviceAccount.Name,
		Namespace:       serviceAccount.Namespace,
		Secrets:         serviceAccount.Secrets,
		Created:         serviceAccount.CreationTimestamp,
		ResourceVersion: serviceAccount.ResourceVersion,
	}

	return data
}

type ServiceAccountQueryParams struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	UID       string `json:"uid"`
	Output    string `json:"output"`
}

type ServiceAccountUpdateParams struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Replicas  int32  `json:"replicas"`
}

func (s *ServiceAccount) List(requestParams interface{}) *utils.Response {
	queryParams := &ServiceAccountQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	list, err := s.KubeClient.InformerRegistry.ServiceAccountInformer().Lister().List(labels.Everything())
	if err != nil {
		return &utils.Response{
			Code: code.ListError,
			Msg:  err.Error(),
		}
	}
	var serviceAccounts []*BuildServiceAccount
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
		serviceAccounts = append(serviceAccounts, s.ToBuildServiceAccount(ds))
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: serviceAccounts}
}

func (s *ServiceAccount) Get(requestParams interface{}) *utils.Response {
	queryParams := &ServiceAccountQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	if queryParams.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "ServiceAccount name is blank"}
	}
	if queryParams.Namespace == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Namespace is blank"}
	}
	serviceAccount, err := s.KubeClient.InformerRegistry.ServiceAccountInformer().Lister().ServiceAccounts(queryParams.Namespace).Get(queryParams.Name)
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
		d, e := runtime.Encode(encoder, serviceAccount)
		if e != nil {
			klog.Error(e)
			return &utils.Response{Code: code.Success, Msg: e.Error()}
		}
		return &utils.Response{Code: code.Success, Msg: "Success", Data: string(d)}
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: serviceAccount}
}

func (s *ServiceAccount) UpdateObj(updateParams interface{}) *utils.Response {
	params := &ServiceAccountUpdateParams{}
	json.Unmarshal(updateParams.([]byte), params)
	if params.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "ServiceAccount name is blank"}
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
		result, getErr := s.KubeClient.InformerRegistry.ServiceAccountInformer().Lister().ServiceAccounts(params.Namespace).Get(params.Name)
		if getErr != nil {
			panic(fmt.Errorf("failed to get latest version of ServiceAccount: %v", getErr))
		}

		//result.Spec.Replicas = &params.Replicas
		_, updateErr := s.ClientSet.CoreV1().ServiceAccounts(params.Namespace).Update(s.context, result, metav1.UpdateOptions{})
		return updateErr
	})
	if retryErr != nil {
		klog.Errorf("Update failed: %v", retryErr)
		return &utils.Response{Code: code.ParamsError, Msg: retryErr.Error()}
	}
	return &utils.Response{Code: code.Success, Msg: "Success"}
}
