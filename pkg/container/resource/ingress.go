package resource

import (
	"encoding/json"
	"fmt"
	"github.com/kubespace/agent/pkg/kubernetes"
	"github.com/kubespace/agent/pkg/utils"
	"github.com/kubespace/agent/pkg/utils/code"
	extv1betav1 "k8s.io/api/extensions/v1beta1"
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

type Ingress struct {
	watch *WatchResource
	*DynamicResource
}

var (
	NetworkIngressGVR = &schema.GroupVersionResource{
		Group:    "networking",
		Version:  "v1",
		Resource: "ingresses",
	}

	ExtensionIngressGVR = &schema.GroupVersionResource{
		Group:    "extensions",
		Version:  "v1beta1",
		Resource: "ingresses",
	}
)

func NewIngress(kubeClient *kubernetes.KubeClient, watch *WatchResource) *Ingress {
	var dynamic *schema.GroupVersionResource
	if kubernetes.VersionGreaterThan19(kubeClient.Version) {
		dynamic = NetworkIngressGVR
	} else {
		dynamic = ExtensionIngressGVR
	}
	s := &Ingress{
		watch:           watch,
		DynamicResource: NewDynamicResource(kubeClient, dynamic),
	}
	s.DoWatch()
	return s
}

func (i *Ingress) DoWatch() {
	if kubernetes.VersionGreaterThan19(i.KubeClient.Version) {
		informer := i.KubeClient.NewIngressInformer().Informer()
		informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    i.watch.WatchAdd(utils.WatchIngress),
			UpdateFunc: i.watch.WatchUpdate(utils.WatchIngress),
			DeleteFunc: i.watch.WatchDelete(utils.WatchIngress),
		})
	} else {
		informer := i.KubeClient.IngressInformer().Informer()
		informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    i.watch.WatchAdd(utils.WatchIngress),
			UpdateFunc: i.watch.WatchUpdate(utils.WatchIngress),
			DeleteFunc: i.watch.WatchDelete(utils.WatchIngress),
		})
	}
}

type BuildIngress struct {
	UID             string                      `json:"uid"`
	Name            string                      `json:"name"`
	Namespace       string                      `json:"namespace"`
	Backend         *extv1betav1.IngressBackend `json:"backend"`
	TLS             []extv1betav1.IngressTLS    `json:"tls"`
	Rules           []extv1betav1.IngressRule   `json:"rules"`
	Created         metav1.Time                 `json:"created"`
	ResourceVersion string                      `json:"resource_version"`
}

func (i *Ingress) ToBuildIngress(ingress *extv1betav1.Ingress) *BuildIngress {
	if ingress == nil {
		return nil
	}
	data := &BuildIngress{
		UID:             string(ingress.UID),
		Name:            ingress.Name,
		Namespace:       ingress.Namespace,
		Backend:         ingress.Spec.Backend,
		TLS:             ingress.Spec.TLS,
		Rules:           ingress.Spec.Rules,
		Created:         ingress.CreationTimestamp,
		ResourceVersion: ingress.ResourceVersion,
	}

	return data
}

type IngressQueryParams struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	UID       string            `json:"uid"`
	Output    string            `json:"output"`
	Labels    map[string]string `json:"labels"`
}

type IngressUpdateParams struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Replicas  int32  `json:"replicas"`
}

func (i *Ingress) List(requestParams interface{}) *utils.Response {
	queryParams := &IngressQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	var selector labels.Selector
	if queryParams.Labels != nil {
		selector = labels.Set(queryParams.Labels).AsSelector()
	} else {
		selector = labels.Everything()
	}
	if kubernetes.VersionGreaterThan19(i.KubeClient.Version) {

		return &utils.Response{Code: code.Success, Msg: "Success", Data: nil}
	} else {
		list, err := i.KubeClient.IngressInformer().Lister().Ingresses(queryParams.Namespace).List(selector)
		if err != nil {
			return &utils.Response{
				Code: code.ListError,
				Msg:  err.Error(),
			}
		}
		var ingresss []*BuildIngress
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
			ingresss = append(ingresss, i.ToBuildIngress(ds))
		}
		return &utils.Response{Code: code.Success, Msg: "Success", Data: ingresss}
	}
}

func (i *Ingress) Get(requestParams interface{}) *utils.Response {
	queryParams := &IngressQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	if queryParams.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Ingress name is blank"}
	}
	if queryParams.Namespace == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Namespace is blank"}
	}
	ingress, err := i.KubeClient.InformerRegistry.IngressInformer().Lister().Ingresses(queryParams.Namespace).Get(queryParams.Name)
	if err != nil {
		return &utils.Response{Code: code.GetError, Msg: err.Error()}
	}
	if queryParams.Output == "yaml" {
		const mediaType = runtime.ContentTypeYAML
		rscheme := runtime.NewScheme()
		extv1betav1.AddToScheme(rscheme)
		codecs := serializer.NewCodecFactory(rscheme)
		info, ok := runtime.SerializerInfoForMediaType(codecs.SupportedMediaTypes(), mediaType)
		if !ok {
			return &utils.Response{Code: code.Success, Msg: fmt.Sprintf("unsupported media type %q", mediaType)}
		}

		encoder := codecs.EncoderForVersion(info.Serializer, i.GroupVersion())
		//klog.Info(a)
		d, e := runtime.Encode(encoder, ingress)
		if e != nil {
			klog.Error(e)
			return &utils.Response{Code: code.GetError, Msg: e.Error()}
		}
		return &utils.Response{Code: code.Success, Msg: "Success", Data: string(d)}
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: ingress}
}

func (i *Ingress) UpdateObj(updateParams interface{}) *utils.Response {
	params := &IngressUpdateParams{}
	json.Unmarshal(updateParams.([]byte), params)
	if params.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Ingress name is blank"}
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
		result, getErr := i.KubeClient.InformerRegistry.IngressInformer().Lister().Ingresses(params.Namespace).Get(params.Name)
		if getErr != nil {
			panic(fmt.Errorf("failed to get latest version of Ingress: %v", getErr))
		}

		//result.Spec.Replicas = &params.Replicas
		_, updateErr := i.ClientSet.ExtensionsV1beta1().Ingresses(params.Namespace).Update(i.context, result, metav1.UpdateOptions{})
		return updateErr
	})
	if retryErr != nil {
		klog.Errorf("Update failed: %v", retryErr)
		return &utils.Response{Code: code.ParamsError, Msg: retryErr.Error()}
	}
	return &utils.Response{Code: code.Success, Msg: "Success"}
}
