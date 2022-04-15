package resource

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/openspacee/ospagent/pkg/kubernetes"
	"github.com/openspacee/ospagent/pkg/utils"
	"github.com/openspacee/ospagent/pkg/utils/code"
	apiExtensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog"
)

type Crd struct {
	*kubernetes.KubeClient
	*DynamicResource
	context context.Context
}

func NewCrd(kubeClient *kubernetes.KubeClient) *Crd {
	n := &Crd{
		KubeClient: kubeClient,
		DynamicResource: NewDynamicResource(kubeClient, &schema.GroupVersionResource{
			Group:    "apiextensions.k8s.io",
			Version:  "v1beta1",
			Resource: "customresourcedefinition",
		}),
		context: context.Background(),
	}
	return n
}

type CrdQueryParams struct {
	Name   string `json:"name"`
	Output string `json:"output"`
}

func (c *Crd) List(requestParams interface{}) *utils.Response {
	crds, err := c.KubeClient.ApiExtensionsClientSet.ApiextensionsV1beta1().CustomResourceDefinitions().List(c.context, metav1.ListOptions{})
	if err != nil {
		return &utils.Response{
			Code: code.ListError,
			Msg:  err.Error(),
		}
	}
	var crdsList []map[string]interface{}
	for _, crd := range crds.Items {
		crdsList = append(crdsList, map[string]interface{}{
			"name":        crd.Name,
			"scope":       crd.Spec.Scope,
			"version":     crd.Spec.Version,
			"group":       crd.Spec.Group,
			"create_time": crd.CreationTimestamp,
		})
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: crdsList}
}

func (c *Crd) Get(requestParams interface{}) *utils.Response {
	queryParams := &CrdQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	if queryParams.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Name is blank"}
	}
	crd, err := c.KubeClient.ApiExtensionsClientSet.ApiextensionsV1beta1().CustomResourceDefinitions().Get(c.context, queryParams.Name, metav1.GetOptions{})
	if err != nil {
		return &utils.Response{Code: code.GetError, Msg: err.Error()}
	}
	if queryParams.Output == "yaml" {
		const mediaType = runtime.ContentTypeYAML
		rscheme := runtime.NewScheme()
		apiExtensionsv1beta1.AddToScheme(rscheme)
		codecs := serializer.NewCodecFactory(rscheme)
		info, ok := runtime.SerializerInfoForMediaType(codecs.SupportedMediaTypes(), mediaType)
		if !ok {
			return &utils.Response{Code: code.Success, Msg: fmt.Sprintf("unsupported media type %q", mediaType)}
		}

		encoder := codecs.EncoderForVersion(info.Serializer, c.GroupVersion())
		d, e := runtime.Encode(encoder, crd)
		if e != nil {
			klog.Error(e)
			return &utils.Response{Code: code.EncodeError, Msg: e.Error()}
		}
		return &utils.Response{Code: code.Success, Msg: "Success", Data: string(d)}
	}

	return &utils.Response{Code: code.Success, Msg: "Success", Data: crd}
}

type CRRequest struct {
	Group    string `json:"group"`
	Resource string `json:"resource"`
	Version  string `json:"version"`
}

func (c *Crd) ListCR(requestParams interface{}) *utils.Response {
	queryParams := &CRRequest{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	if queryParams.Group == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "CR group is blank"}
	}
	if queryParams.Resource == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "CR resource is blank"}
	}
	if queryParams.Version == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "CR version is blank"}
	}
	dynClient, err := dynamic.NewForConfig(c.Config)
	if err != nil {
		return &utils.Response{Code: code.ParamsError, Msg: "New for client error: " + err.Error()}
	}
	gvr := schema.GroupVersionResource{
		Group:    queryParams.Group,
		Version:  queryParams.Version,
		Resource: queryParams.Resource,
	}
	crdClient := dynClient.Resource(gvr)
	crs, err := crdClient.Namespace("").List(c.context, metav1.ListOptions{})
	var crList []map[string]interface{}
	for _, cr := range crs.Items {
		crList = append(crList, map[string]interface{}{
			"name":        cr.GetName(),
			"namespace":   cr.GetNamespace(),
			"create_time": cr.GetCreationTimestamp(),
		})
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: crList}
}
