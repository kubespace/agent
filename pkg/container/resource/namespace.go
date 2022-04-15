package resource

import (
	"encoding/json"
	"fmt"
	"github.com/openspacee/ospagent/pkg/kubernetes"
	"github.com/openspacee/ospagent/pkg/utils"
	"github.com/openspacee/ospagent/pkg/utils/code"
	"github.com/openspacee/ospagent/pkg/websocket"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
	"strings"
)

type Namespace struct {
	websocket.SendResponse
	watch *WatchResource
	*DynamicResource
}

func NewNamespace(
	kubeClient *kubernetes.KubeClient,
	sendResponse websocket.SendResponse,
	watch *WatchResource) *Namespace {

	ns := &Namespace{
		SendResponse: sendResponse,
		watch:        watch,
		DynamicResource: NewDynamicResource(kubeClient, &schema.GroupVersionResource{
			Group:    "",
			Version:  "v1",
			Resource: "namespaces",
		}),
	}
	ns.DoWatch()
	return ns
}

func (n *Namespace) DoWatch() {
	nsInformer := n.KubeClient.NamespaceInformer().Informer()
	nsInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    n.watch.WatchAdd(utils.WatchNamespace),
		UpdateFunc: n.watch.WatchUpdate(utils.WatchNamespace),
		DeleteFunc: n.watch.WatchDelete(utils.WatchNamespace),
	})
}

type NsQueryParams struct {
	Name   string `json:"name"`
	Output string `json:"output"`
}

type BuildNamespace struct {
	UID             string            `json:"uid"`
	Name            string            `json:"name"`
	Created         metav1.Time       `json:"created"`
	Status          string            `json:"status"`
	Labels          map[string]string `json:"labels"`
	ResourceVersion string            `json:"resource_version"`
}

func (n *Namespace) ToBuildNamespace(ns *v1.Namespace) *BuildNamespace {
	if ns == nil {
		return nil
	}
	return &BuildNamespace{
		UID:             string(ns.UID),
		Name:            ns.Name,
		Created:         ns.CreationTimestamp,
		Status:          string(ns.Status.Phase),
		Labels:          ns.Labels,
		ResourceVersion: ns.ResourceVersion,
	}
}

func (n *Namespace) List(requestParams interface{}) *utils.Response {
	queryParams := &NsQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	nsList, err := n.KubeClient.InformerRegistry.NamespaceInformer().Lister().List(labels.Everything())
	if err != nil {
		return &utils.Response{
			Code: code.ListError,
			Msg:  err.Error(),
		}
	}
	var nsRes []*BuildNamespace
	for _, ns := range nsList {
		if queryParams.Name == "" || strings.Contains(ns.ObjectMeta.Name, queryParams.Name) {
			nsRes = append(nsRes, n.ToBuildNamespace(ns))
		}
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: nsRes}
}

func (n *Namespace) Get(requestParams interface{}) *utils.Response {
	queryParams := &NsQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	if queryParams.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Service name is blank"}
	}
	ns, err := n.KubeClient.InformerRegistry.NamespaceInformer().Lister().Get(queryParams.Name)
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

		encoder := codecs.EncoderForVersion(info.Serializer, n.GroupVersion())
		//klog.Info(a)
		d, e := runtime.Encode(encoder, ns)
		if e != nil {
			klog.Error(e)
			return &utils.Response{Code: code.Success, Msg: e.Error()}
		}
		return &utils.Response{Code: code.Success, Msg: "Success", Data: string(d)}
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: ns}
}
