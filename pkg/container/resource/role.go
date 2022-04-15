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

type Role struct {
	*kubernetes.KubeClient
	watch              *WatchResource
	clusterRoleDynamic *DynamicResource
	roleDynamic        *DynamicResource
}

func NewRole(kubeClient *kubernetes.KubeClient, watch *WatchResource) *Role {
	s := &Role{
		KubeClient: kubeClient,
		watch:      watch,
		roleDynamic: NewDynamicResource(kubeClient, &schema.GroupVersionResource{
			Group:    "rbac.authorization.k8s.io",
			Version:  "v1",
			Resource: "roles",
		}),
		clusterRoleDynamic: NewDynamicResource(kubeClient, &schema.GroupVersionResource{
			Group:    "rbac.authorization.k8s.io",
			Version:  "v1",
			Resource: "clusterroles",
		}),
	}
	s.DoWatch()
	return s
}

func (s *Role) DoWatch() {
	informer := s.KubeClient.RoleInformer().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    s.watch.WatchAdd(utils.WatchRole),
		UpdateFunc: s.watch.WatchUpdate(utils.WatchRole),
		DeleteFunc: s.watch.WatchDelete(utils.WatchRole),
	})
	cinformer := s.KubeClient.ClusterRoleInformer().Informer()
	cinformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    s.watch.WatchAdd(utils.WatchRole),
		UpdateFunc: s.watch.WatchUpdate(utils.WatchRole),
		DeleteFunc: s.watch.WatchDelete(utils.WatchRole),
	})
}

type BuildRole struct {
	UID       string `json:"uid"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	//Rules []rbacv1.PolicyRule `json:"rules"`
	ResourceVersion string      `json:"resource_version"`
	Created         metav1.Time `json:"created"`
}

func (s *Role) ToBuildRole(role *rbacv1.Role) *BuildRole {
	if role == nil {
		return nil
	}
	data := &BuildRole{
		UID:       string(role.UID),
		Name:      role.Name,
		Kind:      "Role",
		Namespace: role.Namespace,
		//Rules: role.Rules,
		Created:         role.CreationTimestamp,
		ResourceVersion: role.ResourceVersion,
	}

	return data
}

func (s *Role) ToBuildClusterRole(role *rbacv1.ClusterRole) *BuildRole {
	if role == nil {
		return nil
	}
	data := &BuildRole{
		UID:       string(role.UID),
		Kind:      "ClusterRole",
		Name:      role.Name,
		Namespace: role.Namespace,
		//Rules: role.Rules,
		Created:         role.CreationTimestamp,
		ResourceVersion: role.ResourceVersion,
	}

	return data
}

type RoleQueryParams struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	UID       string `json:"uid"`
	Kind      string `json:"kind"`
	Output    string `json:"output"`
}

type RoleUpdateParams struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Replicas  int32  `json:"replicas"`
}

func (s *Role) List(requestParams interface{}) *utils.Response {
	queryParams := &RoleQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	list, err := s.KubeClient.InformerRegistry.RoleInformer().Lister().List(labels.Everything())
	if err != nil {
		return &utils.Response{
			Code: code.ListError,
			Msg:  err.Error(),
		}
	}
	clist, err := s.KubeClient.InformerRegistry.ClusterRoleInformer().Lister().List(labels.Everything())
	if err != nil {
		return &utils.Response{
			Code: code.ListError,
			Msg:  err.Error(),
		}
	}
	var roles []*BuildRole
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
		roles = append(roles, s.ToBuildRole(ds))
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
		roles = append(roles, s.ToBuildClusterRole(ds))
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: roles}
}

func (s *Role) Get(requestParams interface{}) *utils.Response {
	queryParams := &RoleQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	if queryParams.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Role name is blank"}
	}
	if queryParams.Kind == "" || (queryParams.Kind != "ClusterRole" && queryParams.Kind != "Role") {
		return &utils.Response{Code: code.ParamsError, Msg: "Kind is not correct"}
	}
	if queryParams.Namespace == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Namespace is blank"}
	}
	var role runtime.Object
	var err error
	if queryParams.Kind == "Role" {
		role, err = s.KubeClient.InformerRegistry.RoleInformer().Lister().Roles(queryParams.Namespace).Get(queryParams.Name)
	} else if queryParams.Kind == "ClusterRole" {
		role, err = s.KubeClient.InformerRegistry.ClusterRoleInformer().Lister().Get(queryParams.Name)
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
		if queryParams.Kind == "ClusterRole" {
			encoder = codecs.EncoderForVersion(info.Serializer, s.clusterRoleDynamic.GroupVersion())
		} else {
			encoder = codecs.EncoderForVersion(info.Serializer, s.roleDynamic.GroupVersion())
		}
		//klog.Info(a)
		d, e := runtime.Encode(encoder, role)
		if e != nil {
			klog.Error(e)
			return &utils.Response{Code: code.Success, Msg: e.Error()}
		}
		return &utils.Response{Code: code.Success, Msg: "Success", Data: string(d)}
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: role}
}

func (s *Role) UpdateObj(updateParams interface{}) *utils.Response {
	params := &RoleUpdateParams{}
	json.Unmarshal(updateParams.([]byte), params)
	if params.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Role name is blank"}
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
		result, getErr := s.KubeClient.InformerRegistry.RoleInformer().Lister().Roles(params.Namespace).Get(params.Name)
		if getErr != nil {
			panic(fmt.Errorf("failed to get latest version of Role: %v", getErr))
		}

		//result.Spec.Replicas = &params.Replicas
		_, updateErr := s.ClientSet.RbacV1().Roles(params.Namespace).Update(s.roleDynamic.context, result, metav1.UpdateOptions{})
		return updateErr
	})
	if retryErr != nil {
		klog.Errorf("Update failed: %v", retryErr)
		return &utils.Response{Code: code.ParamsError, Msg: retryErr.Error()}
	}
	return &utils.Response{Code: code.Success, Msg: "Success"}
}

func (s *Role) UpdateYaml(updateParams interface{}) *utils.Response {
	params := &DynamicUpdateParams{}
	json.Unmarshal(updateParams.([]byte), params)
	if params.Kind == "ClusterRole" {
		return s.clusterRoleDynamic.UpdateYaml(updateParams)
	} else if params.Kind == "Role" {
		return s.roleDynamic.UpdateYaml(updateParams)
	}
	return &utils.Response{Code: code.ParamsError, Msg: "Kind parameter is not correct"}
}
