package resource

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kubespace/agent/pkg/kubernetes"
	"github.com/kubespace/agent/pkg/utils"
	"github.com/kubespace/agent/pkg/utils/code"
	apiExtensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiExtensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog"
)

var NewCRDGVR = &schema.GroupVersionResource{
	Group:    "apiextensions.k8s.io",
	Version:  "v1",
	Resource: "customresourcedefinition",
}

var CRDGVR = &schema.GroupVersionResource{
	Group:    "apiextensions.k8s.io",
	Version:  "v1beta1",
	Resource: "customresourcedefinition",
}

type Crd struct {
	*kubernetes.KubeClient
	*DynamicResource
	context context.Context
}

func NewCrd(kubeClient *kubernetes.KubeClient) *Crd {
	crdGvr := CRDGVR
	if kubernetes.VersionGreaterThan16(kubeClient.Version) {
		crdGvr = NewCRDGVR
	}
	n := &Crd{
		KubeClient:      kubeClient,
		DynamicResource: NewDynamicResource(kubeClient, crdGvr),
		context:         context.Background(),
	}
	return n
}

type CrdQueryParams struct {
	Name   string `json:"name"`
	Output string `json:"output"`
}

func (c *Crd) List(requestParams interface{}) *utils.Response {
	if kubernetes.VersionGreaterThan16(c.Version) {
		crds, err := c.KubeClient.ApiExtensionsClientSet.ApiextensionsV1().CustomResourceDefinitions().List(c.context, metav1.ListOptions{})
		if err != nil {
			return &utils.Response{Code: code.ListError, Msg: err.Error()}
		}
		var crdsList []map[string]interface{}
		for _, crd := range crds.Items {
			version := ""
			for _, ver := range crd.Spec.Versions {
				if ver.Storage {
					version = ver.Name
				}
			}
			crdsList = append(crdsList, map[string]interface{}{
				"name":        crd.Name,
				"scope":       crd.Spec.Scope,
				"version":     version,
				"group":       crd.Spec.Group,
				"create_time": crd.CreationTimestamp,
			})
		}
		return &utils.Response{Code: code.Success, Msg: "Success", Data: crdsList}
	} else {
		crds, err := c.KubeClient.ApiExtensionsClientSet.ApiextensionsV1beta1().CustomResourceDefinitions().List(c.context, metav1.ListOptions{})
		if err != nil {
			return &utils.Response{Code: code.ListError, Msg: err.Error()}
		}
		var crdsList []map[string]interface{}
		for _, crd := range crds.Items {
			version := crd.Spec.Version
			for _, ver := range crd.Spec.Versions {
				if ver.Storage {
					version = ver.Name
				}
			}
			crdsList = append(crdsList, map[string]interface{}{
				"name":        crd.Name,
				"scope":       crd.Spec.Scope,
				"version":     version,
				"group":       crd.Spec.Group,
				"create_time": crd.CreationTimestamp,
			})
		}
		return &utils.Response{Code: code.Success, Msg: "Success", Data: crdsList}
	}
}

func (c *Crd) Get(requestParams interface{}) *utils.Response {
	queryParams := &CrdQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	if queryParams.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Name is blank"}
	}
	var crd runtime.Object
	var err error
	if kubernetes.VersionGreaterThan16(c.KubeClient.Version) {
		crd, err = c.KubeClient.ApiExtensionsClientSet.ApiextensionsV1().CustomResourceDefinitions().Get(c.context, queryParams.Name, metav1.GetOptions{})
	} else {
		crd, err = c.KubeClient.ApiExtensionsClientSet.ApiextensionsV1beta1().CustomResourceDefinitions().Get(c.context, queryParams.Name, metav1.GetOptions{})
	}
	if err != nil {
		return &utils.Response{Code: code.GetError, Msg: err.Error()}
	}
	if queryParams.Output == "yaml" {
		const mediaType = runtime.ContentTypeYAML
		rscheme := runtime.NewScheme()
		if kubernetes.VersionGreaterThan16(c.KubeClient.Version) {
			apiExtensionsv1.AddToScheme(rscheme)
		} else {
			apiExtensionsv1beta1.AddToScheme(rscheme)
		}
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
