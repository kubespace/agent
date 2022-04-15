package resource

import (
	"encoding/json"
	"fmt"
	"github.com/kubespace/agent/pkg/kubernetes"
	"github.com/kubespace/agent/pkg/utils"
	"github.com/kubespace/agent/pkg/utils/code"
	"github.com/kubespace/agent/pkg/websocket"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog"
)

type ConfigMap struct {
	*kubernetes.KubeClient
	websocket.SendResponse
	*DynamicResource
}

type BuildConfigMap struct {
	Name       string            `json:"name"`
	NameSpace  string            `json:"namespace"`
	Keys       []string          `json:"keys"`
	Labels     map[string]string `json:"labels"`
	CreateTime string            `json:"create_time"`
	Data       map[string]string `json:"data"`
}

type ConfigMapQueryParams struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Output    string            `json:"output"`
	Labels    map[string]string `json:"labels"`
}

type ConfigMapUpdateParams struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Data      map[string]string `json:"data"`
}

type ConfigMapCreateParams struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Data      map[string]string `json:"data"`
}

func (c *ConfigMap) ToBuildConfigMap(cm *v1.ConfigMap) *BuildConfigMap {
	if cm == nil {
		return nil
	}

	cmData := &BuildConfigMap{
		Name:       cm.Name,
		NameSpace:  cm.Namespace,
		Labels:     cm.Labels,
		CreateTime: fmt.Sprint(cm.CreationTimestamp),
		Data:       cm.Data,
	}

	keys := make([]string, 0, len(cm.Data))
	for k, _ := range cm.Data {
		keys = append(keys, k)
	}
	cmData.Keys = keys
	return cmData
}

func NewConfigMap(kubeClient *kubernetes.KubeClient, sendResponse websocket.SendResponse) *ConfigMap {
	return &ConfigMap{
		KubeClient:   kubeClient,
		SendResponse: sendResponse,
		DynamicResource: NewDynamicResource(kubeClient, &schema.GroupVersionResource{
			Group:    "",
			Version:  "v1",
			Resource: "configmaps",
		}),
	}
}

func (c *ConfigMap) List(requestParams interface{}) *utils.Response {
	queryParams := &ConfigMapQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	var selector labels.Selector
	if queryParams.Labels != nil {
		selector = labels.Set(queryParams.Labels).AsSelector()
	} else {
		selector = labels.Everything()
	}
	configMapList, err := c.KubeClient.ConfigMapInformer().Lister().ConfigMaps(queryParams.Namespace).List(selector)
	if err != nil {
		return &utils.Response{
			Code: code.ListError,
			Msg:  err.Error(),
		}
	}
	var configMapResource []*BuildConfigMap
	for _, cm := range configMapList {
		configMapResource = append(configMapResource, c.ToBuildConfigMap(cm))
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: configMapResource}
}

func (c *ConfigMap) Get(requestParams interface{}) *utils.Response {
	queryParams := &ConfigMapQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	if queryParams.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Name is blank"}
	}
	if queryParams.Namespace == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Namespace is blank"}
	}
	configMap, err := c.KubeClient.ConfigMapInformer().Lister().ConfigMaps(queryParams.Namespace).Get(queryParams.Name)
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

		encoder := codecs.EncoderForVersion(info.Serializer, c.GroupVersion())
		d, e := runtime.Encode(encoder, configMap)
		if e != nil {
			klog.Error(e)
			return &utils.Response{Code: code.EncodeError, Msg: e.Error()}
		}
		klog.Info(d)
		return &utils.Response{Code: code.Success, Msg: "Success", Data: string(d)}
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: configMap}
}

func (c *ConfigMap) UpdateObj(updateParams interface{}) *utils.Response {
	params := &ConfigMapUpdateParams{}
	json.Unmarshal(updateParams.([]byte), params)

	if params.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "ConfigMap name is blank"}
	}
	if params.Namespace == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Namespace is blank"}
	}
	if len(params.Data) == 0 {
		return &utils.Response{Code: code.ParamsError, Msg: "Data is blank"}
	}
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Retrieve the latest version of Deployment before attempting update
		// RetryOnConflict uses exponential backoff to avoid exhausting the apiserver
		result, getErr := c.KubeClient.InformerRegistry.ConfigMapInformer().Lister().ConfigMaps(params.Namespace).Get(params.Name)
		if getErr != nil {
			panic(fmt.Errorf("failed to get latest version of Deployment: %v", getErr))
		}

		result.Data = params.Data
		_, updateErr := c.ClientSet.CoreV1().ConfigMaps(params.Namespace).Update(c.context, result, metav1.UpdateOptions{})
		return updateErr
	})
	if retryErr != nil {
		klog.Errorf("Update failed: %v", retryErr)
		return &utils.Response{Code: code.ParamsError, Msg: retryErr.Error()}
	}
	return &utils.Response{Code: code.Success, Msg: "Success"}
}

func (c *ConfigMap) Create(createParams interface{}) *utils.Response {

	params := &ConfigMapUpdateParams{}
	json.Unmarshal(createParams.([]byte), params)

	configMap := v1.ConfigMap{}
	configMap.APIVersion = "v1"
	configMap.Kind = "ConfigMap"
	configMap.Name = params.Name
	configMap.Data = params.Data
	configMap.Namespace = params.Namespace

	cm, err := c.ClientSet.CoreV1().ConfigMaps(params.Namespace).Create(c.context, &configMap, metav1.CreateOptions{})
	if err != nil {
		klog.Errorf("Create ConfigMap failed: %v", err)
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: cm}
}
