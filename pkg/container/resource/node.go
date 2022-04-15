package resource

import (
	"encoding/json"
	"fmt"
	"github.com/openspacee/ospagent/pkg/kubernetes"
	"github.com/openspacee/ospagent/pkg/utils"
	"github.com/openspacee/ospagent/pkg/utils/code"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
	"math"
	"time"
)

type Node struct {
	*kubernetes.KubeClient
	watch *WatchResource
	*DynamicResource
}

func NewNode(kubeClient *kubernetes.KubeClient, watch *WatchResource) *Node {
	n := &Node{
		KubeClient: kubeClient,
		watch:      watch,
		DynamicResource: NewDynamicResource(kubeClient, &schema.GroupVersionResource{
			Group:    "",
			Version:  "v1",
			Resource: "nodes",
		}),
	}
	n.DoWatch()
	return n
}

func (n *Node) DoWatch() {
	informer := n.KubeClient.NodeInformer().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    n.watch.WatchAdd(utils.WatchNode),
		UpdateFunc: n.watch.WatchUpdate(utils.WatchNode),
		DeleteFunc: n.watch.WatchDelete(utils.WatchNode),
	})
}

type BuildNode struct {
	UID              string            `json:"uid"`
	Name             string            `json:"name"`
	Taints           int               `json:"taints"`
	Roles            string            `json:"roles"`
	Version          string            `json:"version"`
	Age              string            `json:"age"`
	Status           string            `json:"status"`
	OS               string            `json:"os"`
	OSImage          string            `json:"os_image"`
	KernelVersion    string            `json:"kernel_version"`
	ContainerRuntime string            `json:"container_runtime"`
	Labels           map[string]string `json:"labels"`
	TotalCPU         string            `json:"total_cpu"`
	AllocatableCpu   string            `json:"allocatable_cpu"`
	TotalMem         string            `json:"total_mem"`
	AllocatableMem   string            `json:"allocatable_mem"`
	InternalIP       string            `json:"internal_ip"`
	Created          metav1.Time       `json:"created"`
}

func (n *Node) ToBuildNode(node *v1.Node) *BuildNode {
	if node == nil {
		return nil
	}
	nodeData := &BuildNode{
		UID:              string(node.UID),
		Name:             node.Name,
		Taints:           len(node.Spec.Taints),
		Version:          node.Status.NodeInfo.KubeletVersion,
		OS:               node.Status.NodeInfo.OperatingSystem,
		OSImage:          node.Status.NodeInfo.OSImage,
		KernelVersion:    node.Status.NodeInfo.KernelVersion,
		ContainerRuntime: node.Status.NodeInfo.ContainerRuntimeVersion,
		Labels:           node.Labels,
		AllocatableCpu:   node.Status.Allocatable.Cpu().String(),
		TotalCPU:         node.Status.Capacity.Cpu().String(),
		AllocatableMem:   node.Status.Allocatable.Memory().String(),
		TotalMem:         node.Status.Capacity.Memory().String(),
		Created:          node.CreationTimestamp,
	}
	dur := time.Now().Sub(node.CreationTimestamp.Time)
	nodeData.Age = fmt.Sprintf("%vd", math.Floor(dur.Hours()/24))

	for _, c := range node.Status.Conditions {
		if c.Type == "Ready" && c.Status == v1.ConditionTrue {
			nodeData.Status = "Ready"
		} else {
			nodeData.Status = "NotReady"
		}
	}
	for _, i := range node.Status.Addresses {
		if i.Type == v1.NodeInternalIP {
			nodeData.InternalIP = i.Address
		}
	}

	return nodeData
}

type NodeQueryParams struct {
	Name   string `json:"name"`
	Output string `json:"output"`
}

func (n *Node) List(requestParams interface{}) *utils.Response {
	nodeList, err := n.KubeClient.InformerRegistry.NodeInformer().Lister().List(labels.Everything())
	if err != nil {
		return &utils.Response{
			Code: code.ListError,
			Msg:  err.Error(),
		}
	}
	var nodeResource []*BuildNode
	for _, node := range nodeList {
		nodeResource = append(nodeResource, n.ToBuildNode(node))
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: nodeResource}
}

func (n *Node) Get(requestParams interface{}) *utils.Response {
	queryParams := &NodeQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	if queryParams.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Name is blank"}
	}
	sc, err := n.KubeClient.NodeInformer().Lister().Get(queryParams.Name)
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
		d, e := runtime.Encode(encoder, sc)
		if e != nil {
			klog.Error(e)
			return &utils.Response{Code: code.EncodeError, Msg: e.Error()}
		}
		return &utils.Response{Code: code.Success, Msg: "Success", Data: string(d)}
	}

	return &utils.Response{Code: code.Success, Msg: "Success", Data: sc}
}
