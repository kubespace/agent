package resource

import (
	"encoding/json"
	"fmt"
	"github.com/openspacee/ospagent/pkg/kubernetes"
	"github.com/openspacee/ospagent/pkg/utils"
	"github.com/openspacee/ospagent/pkg/utils/code"
	"k8s.io/api/apps/v1"
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

type StatefulSet struct {
	watch *WatchResource
	*DynamicResource
}

func NewStatefulSet(kubeClient *kubernetes.KubeClient, watch *WatchResource) *StatefulSet {
	s := &StatefulSet{
		watch: watch,
		DynamicResource: NewDynamicResource(kubeClient, &schema.GroupVersionResource{
			Group:    "apps",
			Version:  "v1",
			Resource: "statefulsets",
		}),
	}
	s.DoWatch()
	return s
}

func (s *StatefulSet) DoWatch() {
	sInformer := s.KubeClient.StatefulSetInformer().Informer()
	sInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    s.watch.WatchAdd(utils.WatchStatefulset),
		UpdateFunc: s.watch.WatchUpdate(utils.WatchStatefulset),
		DeleteFunc: s.watch.WatchDelete(utils.WatchStatefulset),
	})
}

type BuildStatefulSet struct {
	UID             string      `json:"uid"`
	Name            string      `json:"name"`
	Namespace       string      `json:"namespace"`
	Replicas        int32       `json:"replicas"`
	StatusReplicas  int32       `json:"status_replicas"`
	ReadyReplicas   int32       `json:"ready_replicas"`
	UpdatedReplicas int32       `json:"updated_replicas"`
	ResourceVersion string      `json:"resource_version"`
	Strategy        string      `json:"strategy"`
	Conditions      []string    `json:"conditions"`
	Created         metav1.Time `json:"created"`
}

func (s *StatefulSet) ToBuildStatefulSet(ss *v1.StatefulSet) *BuildStatefulSet {
	if ss == nil {
		return nil
	}
	var conditions []string
	for _, c := range ss.Status.Conditions {
		if c.Status == corev1.ConditionTrue {
			conditions = append(conditions, string(c.Type))
		}
	}
	data := &BuildStatefulSet{
		UID:             string(ss.UID),
		Name:            ss.Name,
		Namespace:       ss.Namespace,
		Replicas:        *ss.Spec.Replicas,
		StatusReplicas:  ss.Status.Replicas,
		ReadyReplicas:   ss.Status.ReadyReplicas,
		UpdatedReplicas: ss.Status.UpdatedReplicas,
		ResourceVersion: ss.ResourceVersion,
		Strategy:        string(ss.Spec.UpdateStrategy.Type),
		Conditions:      conditions,
		Created:         ss.CreationTimestamp,
	}

	return data
}

type StatefulSetQueryParams struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	UID       string `json:"uid"`
	Output    string `json:"output"`
}

type StatefulSetUpdateParams struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Replicas  int32  `json:"replicas"`
}

func (s *StatefulSet) List(requestParams interface{}) *utils.Response {
	queryParams := &StatefulSetQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	ssList, err := s.KubeClient.InformerRegistry.StatefulSetInformer().Lister().List(labels.Everything())
	if err != nil {
		return &utils.Response{
			Code: code.ListError,
			Msg:  err.Error(),
		}
	}
	var bss []*BuildStatefulSet
	for _, ss := range ssList {
		if queryParams.UID != "" && string(ss.UID) != queryParams.UID {
			continue
		}
		if queryParams.Namespace != "" && ss.Namespace != queryParams.Namespace {
			continue
		}
		if queryParams.Name != "" && strings.Contains(ss.Name, queryParams.Name) {
			continue
		}
		bss = append(bss, s.ToBuildStatefulSet(ss))
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: bss}
}

func (s *StatefulSet) Get(requestParams interface{}) *utils.Response {
	queryParams := &StatefulSetQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	if queryParams.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "StatefulSet name is blank"}
	}
	if queryParams.Namespace == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Namespace is blank"}
	}
	ss, err := s.KubeClient.InformerRegistry.StatefulSetInformer().Lister().StatefulSets(queryParams.Namespace).Get(queryParams.Name)
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

		encoder := codecs.EncoderForVersion(info.Serializer, s.GroupVersion())
		//klog.Info(a)
		d, e := runtime.Encode(encoder, ss)
		if e != nil {
			klog.Error(e)
			return &utils.Response{Code: code.Success, Msg: e.Error()}
		}
		return &utils.Response{Code: code.Success, Msg: "Success", Data: string(d)}
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: ss}
}

func (s *StatefulSet) UpdateObj(updateParams interface{}) *utils.Response {
	params := &StatefulSetUpdateParams{}
	json.Unmarshal(updateParams.([]byte), params)
	if params.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "StatefulSet name is blank"}
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
		result, getErr := s.KubeClient.InformerRegistry.StatefulSetInformer().Lister().StatefulSets(params.Namespace).Get(params.Name)
		if getErr != nil {
			panic(fmt.Errorf("failed to get latest version of StatefulSets: %v", getErr))
		}

		result.Spec.Replicas = &params.Replicas
		_, updateErr := s.ClientSet.AppsV1().StatefulSets(params.Namespace).Update(s.context, result, metav1.UpdateOptions{})
		return updateErr
	})
	if retryErr != nil {
		klog.Errorf("Update failed: %v", retryErr)
		return &utils.Response{Code: code.ParamsError, Msg: retryErr.Error()}
	}
	return &utils.Response{Code: code.Success, Msg: "Success"}
}
