package resource

import (
	"encoding/json"
	"fmt"
	"github.com/openspacee/ospagent/pkg/kubernetes"
	"github.com/openspacee/ospagent/pkg/utils"
	"github.com/openspacee/ospagent/pkg/utils/code"
	rbacv1 "k8s.io/api/rbac/v1"
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

type RoleBinding struct {
	*kubernetes.KubeClient
	watch                     *WatchResource
	clusterRoleBindingDynamic *DynamicResource
	roleBindingDynamic        *DynamicResource
}

func NewRoleBinding(kubeClient *kubernetes.KubeClient, watch *WatchResource) *RoleBinding {
	s := &RoleBinding{
		KubeClient: kubeClient,
		watch:      watch,
		roleBindingDynamic: NewDynamicResource(kubeClient, &schema.GroupVersionResource{
			Group:    "rbac.authorization.k8s.io",
			Version:  "v1",
			Resource: "rolebindings",
		}),
		clusterRoleBindingDynamic: NewDynamicResource(kubeClient, &schema.GroupVersionResource{
			Group:    "rbac.authorization.k8s.io",
			Version:  "v1",
			Resource: "clusterrolebindings",
		}),
	}
	s.DoWatch()
	return s
}

func (s *RoleBinding) DoWatch() {
	informer := s.KubeClient.RoleBindingInformer().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    s.watch.WatchAdd(utils.WatchRoleBinding),
		UpdateFunc: s.watch.WatchUpdate(utils.WatchRoleBinding),
		DeleteFunc: s.watch.WatchDelete(utils.WatchRoleBinding),
	})
	cinformer := s.KubeClient.ClusterRoleBindingInformer().Informer()
	cinformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    s.watch.WatchAdd(utils.WatchRoleBinding),
		UpdateFunc: s.watch.WatchUpdate(utils.WatchRoleBinding),
		DeleteFunc: s.watch.WatchDelete(utils.WatchRoleBinding),
	})
}

type BuildRoleBinding struct {
	UID             string           `json:"uid"`
	Kind            string           `json:"kind"`
	Name            string           `json:"name"`
	Namespace       string           `json:"namespace"`
	Subjects        []rbacv1.Subject `json:"subjects"`
	Role            rbacv1.RoleRef   `json:"role"`
	ResourceVersion string           `json:"resource_version"`
	Created         metav1.Time      `json:"created"`
}

func (s *RoleBinding) ToBuildRoleBinding(roleBinding *rbacv1.RoleBinding) *BuildRoleBinding {
	if roleBinding == nil {
		return nil
	}
	data := &BuildRoleBinding{
		UID:             string(roleBinding.UID),
		Name:            roleBinding.Name,
		Kind:            "RoleBinding",
		Subjects:        roleBinding.Subjects,
		Role:            roleBinding.RoleRef,
		Namespace:       roleBinding.Namespace,
		Created:         roleBinding.CreationTimestamp,
		ResourceVersion: roleBinding.ResourceVersion,
	}

	return data
}

func (s *RoleBinding) ToBuildClusterRoleBinding(roleBinding *rbacv1.ClusterRoleBinding) *BuildRoleBinding {
	if roleBinding == nil {
		return nil
	}
	data := &BuildRoleBinding{
		UID:             string(roleBinding.UID),
		Kind:            "ClusterRoleBinding",
		Name:            roleBinding.Name,
		Namespace:       roleBinding.Namespace,
		Subjects:        roleBinding.Subjects,
		Role:            roleBinding.RoleRef,
		Created:         roleBinding.CreationTimestamp,
		ResourceVersion: roleBinding.ResourceVersion,
	}

	return data
}

type RoleBindingQueryParams struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	UID       string `json:"uid"`
	Kind      string `json:"kind"`
	Output    string `json:"output"`
}

type RoleBindingUpdateParams struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Replicas  int32  `json:"replicas"`
}

func (s *RoleBinding) List(requestParams interface{}) *utils.Response {
	queryParams := &RoleBindingQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	list, err := s.KubeClient.InformerRegistry.RoleBindingInformer().Lister().List(labels.Everything())
	if err != nil {
		return &utils.Response{
			Code: code.ListError,
			Msg:  err.Error(),
		}
	}
	clist, err := s.KubeClient.InformerRegistry.ClusterRoleBindingInformer().Lister().List(labels.Everything())
	if err != nil {
		return &utils.Response{
			Code: code.ListError,
			Msg:  err.Error(),
		}
	}
	var roleBindings []*BuildRoleBinding
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
		roleBindings = append(roleBindings, s.ToBuildRoleBinding(ds))
	}
	for _, ds := range clist {
		if queryParams.UID != "" && string(ds.UID) != queryParams.UID {
			continue
		}
		if queryParams.Namespace != "" && ds.Namespace != queryParams.Namespace {
			continue
		}
		if queryParams.Name != "" && strings.Contains(ds.Name, queryParams.Name) {
			continue
		}
		roleBindings = append(roleBindings, s.ToBuildClusterRoleBinding(ds))
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: roleBindings}
}

func (s *RoleBinding) Get(requestParams interface{}) *utils.Response {
	queryParams := &RoleBindingQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	if queryParams.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "RoleBinding name is blank"}
	}
	if queryParams.Kind == "" || (queryParams.Kind != "ClusterRoleBinding" && queryParams.Kind != "RoleBinding") {
		return &utils.Response{Code: code.ParamsError, Msg: "Kind is not correct"}
	}
	if queryParams.Namespace == "" && queryParams.Kind == "RoleBinding" {
		return &utils.Response{Code: code.ParamsError, Msg: "Namespace is blank"}
	}
	var roleBinding runtime.Object
	var err error
	if queryParams.Kind == "RoleBinding" {
		roleBinding, err = s.KubeClient.InformerRegistry.RoleBindingInformer().Lister().RoleBindings(queryParams.Namespace).Get(queryParams.Name)
	} else if queryParams.Kind == "ClusterRoleBinding" {
		roleBinding, err = s.KubeClient.InformerRegistry.ClusterRoleBindingInformer().Lister().Get(queryParams.Name)
	}
	if err != nil {
		return &utils.Response{Code: code.GetError, Msg: err.Error()}
	}
	if queryParams.Output == "yaml" {
		const mediaType = runtime.ContentTypeYAML
		rscheme := runtime.NewScheme()
		rbacv1.AddToScheme(rscheme)
		codecs := serializer.NewCodecFactory(rscheme)
		info, ok := runtime.SerializerInfoForMediaType(codecs.SupportedMediaTypes(), mediaType)
		if !ok {
			return &utils.Response{Code: code.Success, Msg: fmt.Sprintf("unsupported media type %q", mediaType)}
		}
		var encoder runtime.Encoder
		if queryParams.Kind == "ClusterRoleBinding" {
			encoder = codecs.EncoderForVersion(info.Serializer, s.clusterRoleBindingDynamic.GroupVersion())
		} else {
			encoder = codecs.EncoderForVersion(info.Serializer, s.roleBindingDynamic.GroupVersion())
		}
		//klog.Info(a)
		d, e := runtime.Encode(encoder, roleBinding)
		if e != nil {
			klog.Error(e)
			return &utils.Response{Code: code.Success, Msg: e.Error()}
		}
		return &utils.Response{Code: code.Success, Msg: "Success", Data: string(d)}
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: roleBinding}
}

func (s *RoleBinding) UpdateObj(updateParams interface{}) *utils.Response {
	params := &RoleBindingUpdateParams{}
	json.Unmarshal(updateParams.([]byte), params)
	if params.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "RoleBinding name is blank"}
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
		result, getErr := s.KubeClient.InformerRegistry.RoleBindingInformer().Lister().RoleBindings(params.Namespace).Get(params.Name)
		if getErr != nil {
			panic(fmt.Errorf("failed to get latest version of RoleBinding: %v", getErr))
		}

		//result.Spec.Replicas = &params.Replicas
		_, updateErr := s.ClientSet.RbacV1().RoleBindings(params.Namespace).Update(s.roleBindingDynamic.context, result, metav1.UpdateOptions{})
		return updateErr
	})
	if retryErr != nil {
		klog.Errorf("Update failed: %v", retryErr)
		return &utils.Response{Code: code.ParamsError, Msg: retryErr.Error()}
	}
	return &utils.Response{Code: code.Success, Msg: "Success"}
}

func (s *RoleBinding) UpdateYaml(updateParams interface{}) *utils.Response {
	params := &DynamicUpdateParams{}
	json.Unmarshal(updateParams.([]byte), params)
	if params.Kind == "ClusterRoleBinding" {
		return s.clusterRoleBindingDynamic.UpdateYaml(updateParams)
	} else if params.Kind == "RoleBinding" {
		return s.roleBindingDynamic.UpdateYaml(updateParams)
	}
	return &utils.Response{Code: code.ParamsError, Msg: "Kind parameter is not correct"}
}
