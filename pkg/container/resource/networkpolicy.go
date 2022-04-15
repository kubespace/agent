package resource

import (
	"encoding/json"
	"fmt"
	"github.com/kubespace/agent/pkg/kubernetes"
	"github.com/kubespace/agent/pkg/utils"
	"github.com/kubespace/agent/pkg/utils/code"
	networkv1 "k8s.io/api/networking/v1"
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

type NetworkPolicy struct {
	watch *WatchResource
	*DynamicResource
}

func NewNetworkPolicy(kubeClient *kubernetes.KubeClient, watch *WatchResource) *NetworkPolicy {
	n := &NetworkPolicy{
		watch: watch,
		DynamicResource: NewDynamicResource(kubeClient, &schema.GroupVersionResource{
			Group:    "",
			Version:  "v1",
			Resource: "networkpolicies",
		}),
	}
	n.DoWatch()
	return n
}

func (n *NetworkPolicy) DoWatch() {
	informer := n.KubeClient.NetworkPolicyInformer().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    n.watch.WatchAdd(utils.WatchNetworkPolicy),
		UpdateFunc: n.watch.WatchUpdate(utils.WatchNetworkPolicy),
		DeleteFunc: n.watch.WatchDelete(utils.WatchNetworkPolicy),
	})
}

type BuildNetworkPolicy struct {
	UID             string                 `json:"uid"`
	Name            string                 `json:"name"`
	Namespace       string                 `json:"namespace"`
	PolicyTypes     []networkv1.PolicyType `json:"policy_types"`
	Created         metav1.Time            `json:"created"`
	ResourceVersion string                 `json:"resource_version"`
}

func (n *NetworkPolicy) ToBuildNetworkPolicy(networkpolicy *networkv1.NetworkPolicy) *BuildNetworkPolicy {
	if networkpolicy == nil {
		return nil
	}
	data := &BuildNetworkPolicy{
		UID:             string(networkpolicy.UID),
		Name:            networkpolicy.Name,
		Namespace:       networkpolicy.Namespace,
		PolicyTypes:     networkpolicy.Spec.PolicyTypes,
		Created:         networkpolicy.CreationTimestamp,
		ResourceVersion: networkpolicy.ResourceVersion,
	}

	return data
}

type NetworkPolicyQueryParams struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	UID       string `json:"uid"`
	Output    string `json:"output"`
}

type NetworkPolicyUpdateParams struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Replicas  int32  `json:"replicas"`
}

func (n *NetworkPolicy) List(requestParams interface{}) *utils.Response {
	queryParams := &NetworkPolicyQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	list, err := n.KubeClient.InformerRegistry.NetworkPolicyInformer().Lister().List(labels.Everything())
	if err != nil {
		return &utils.Response{
			Code: code.ListError,
			Msg:  err.Error(),
		}
	}
	var networkpolicies []*BuildNetworkPolicy
	for _, np := range list {
		if queryParams.UID != "" && string(np.UID) != queryParams.UID {
			continue
		}
		if queryParams.Namespace != "" && np.Namespace != queryParams.Namespace {
			continue
		}
		if queryParams.Name != "" && strings.Contains(np.Name, queryParams.Name) {
			continue
		}
		networkpolicies = append(networkpolicies, n.ToBuildNetworkPolicy(np))
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: networkpolicies}
}

func (n *NetworkPolicy) Get(requestParams interface{}) *utils.Response {
	queryParams := &NetworkPolicyQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	if queryParams.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "NetworkPolicy name is blank"}
	}
	if queryParams.Namespace == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Namespace is blank"}
	}
	networkpolicy, err := n.KubeClient.InformerRegistry.NetworkPolicyInformer().Lister().NetworkPolicies(queryParams.Namespace).Get(queryParams.Name)
	if err != nil {
		return &utils.Response{Code: code.GetError, Msg: err.Error()}
	}
	if queryParams.Output == "yaml" {
		const mediaType = runtime.ContentTypeYAML
		rscheme := runtime.NewScheme()
		networkv1.AddToScheme(rscheme)
		codecs := serializer.NewCodecFactory(rscheme)
		info, ok := runtime.SerializerInfoForMediaType(codecs.SupportedMediaTypes(), mediaType)
		if !ok {
			return &utils.Response{Code: code.Success, Msg: fmt.Sprintf("unsupported media type %q", mediaType)}
		}

		encoder := codecs.EncoderForVersion(info.Serializer, n.GroupVersion())
		//klog.Info(a)
		d, e := runtime.Encode(encoder, networkpolicy)
		if e != nil {
			klog.Error(e)
			return &utils.Response{Code: code.Success, Msg: e.Error()}
		}
		return &utils.Response{Code: code.Success, Msg: "Success", Data: string(d)}
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: networkpolicy}
}

func (n *NetworkPolicy) UpdateObj(updateParams interface{}) *utils.Response {
	params := &NetworkPolicyUpdateParams{}
	json.Unmarshal(updateParams.([]byte), params)
	if params.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "NetworkPolicy name is blank"}
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
		result, getErr := n.KubeClient.InformerRegistry.NetworkPolicyInformer().Lister().NetworkPolicies(params.Namespace).Get(params.Name)
		if getErr != nil {
			panic(fmt.Errorf("failed to get latest version of NetworkPolicy: %v", getErr))
		}

		//result.Spec.Replicas = &paramn.Replicas
		_, updateErr := n.ClientSet.NetworkingV1().NetworkPolicies(params.Namespace).Update(n.context, result, metav1.UpdateOptions{})
		return updateErr
	})
	if retryErr != nil {
		klog.Errorf("Update failed: %v", retryErr)
		return &utils.Response{Code: code.ParamsError, Msg: retryErr.Error()}
	}
	return &utils.Response{Code: code.Success, Msg: "Success"}
}
