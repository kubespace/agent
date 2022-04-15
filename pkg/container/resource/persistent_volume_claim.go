package resource

import (
	"encoding/json"
	"fmt"
	"github.com/kubespace/agent/pkg/kubernetes"
	"github.com/kubespace/agent/pkg/utils"
	"github.com/kubespace/agent/pkg/utils/code"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

type PersistentVolumeClaim struct {
	*kubernetes.KubeClient
	watch *WatchResource
	*DynamicResource
}

type BuildPersistentVolumeClaim struct {
	UID           string                           `json:"uid"`
	Name          string                           `json:"name"`
	Namespace     string                           `json:"namespace"`
	Status        string                           `json:"status"`
	StorageClass  *string                          `json:"storage_class"`
	Capacity      string                           `json:"capacity"`
	AccessModes   []v1.PersistentVolumeAccessMode  `json:"access_modes"`
	CreateTime    metav1.Time                      `json:"create_time"`
	ReclaimPolicy v1.PersistentVolumeReclaimPolicy `json:"reclaim_policy"`
}

func NewPersistentVolumeClaim(kubeClient *kubernetes.KubeClient, watch *WatchResource) *PersistentVolumeClaim {
	pvc := &PersistentVolumeClaim{
		KubeClient: kubeClient,
		watch:      watch,
		DynamicResource: NewDynamicResource(kubeClient, &schema.GroupVersionResource{
			Group:    "",
			Version:  "v1",
			Resource: "persistentvolumeclaims",
		}),
	}
	pvc.DoWatch()
	return pvc
}

func (p *PersistentVolumeClaim) DoWatch() {
	informer := p.KubeClient.PersistentVolumeClaimInformer().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    p.watch.WatchAdd(utils.WatchPvc),
		UpdateFunc: p.watch.WatchUpdate(utils.WatchPvc),
		DeleteFunc: p.watch.WatchDelete(utils.WatchPvc),
	})
}

func (p *PersistentVolumeClaim) ToBuildPersistentVolumeClaim(pvc *v1.PersistentVolumeClaim) *BuildPersistentVolumeClaim {
	if pvc == nil {
		return nil
	}
	var volumeSize string
	if size, ok := pvc.Spec.Resources.Requests["storage"]; !ok {
		volumeSize = ""
	} else {
		volumeSize = size.String()
	}

	storageClass := pvc.Spec.StorageClassName

	pvcData := &BuildPersistentVolumeClaim{
		UID:          string(pvc.UID),
		Name:         pvc.Name,
		Status:       string(pvc.Status.Phase),
		AccessModes:  pvc.Spec.AccessModes,
		CreateTime:   pvc.CreationTimestamp,
		Namespace:    pvc.Namespace,
		StorageClass: storageClass,
		Capacity:     volumeSize,
	}

	return pvcData
}

func (p *PersistentVolumeClaim) List(requestParams interface{}) *utils.Response {
	queryParams := &QueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	var selector labels.Selector
	if queryParams.Labels != nil {
		selector = labels.Set(queryParams.Labels).AsSelector()
	} else {
		selector = labels.Everything()
	}
	persistentVolumeClaimList, err := p.KubeClient.PersistentVolumeClaimInformer().Lister().PersistentVolumeClaims(queryParams.Namespace).List(selector)
	if err != nil {
		return &utils.Response{
			Code: code.ListError,
			Msg:  err.Error(),
		}
	}
	var persistentVolumeClaimResource []*BuildPersistentVolumeClaim
	for _, pvc := range persistentVolumeClaimList {
		persistentVolumeClaimResource = append(persistentVolumeClaimResource, p.ToBuildPersistentVolumeClaim(pvc))
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: persistentVolumeClaimResource}
}

func (p *PersistentVolumeClaim) Get(requestParams interface{}) *utils.Response {
	queryParams := &ConfigMapQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	if queryParams.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Name is blank"}
	}
	if queryParams.Namespace == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Namespace is blank"}
	}
	pvc, err := p.KubeClient.InformerRegistry.PersistentVolumeClaimInformer().Lister().PersistentVolumeClaims(queryParams.Namespace).Get(queryParams.Name)
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

		encoder := codecs.EncoderForVersion(info.Serializer, p.GroupVersion())
		d, e := runtime.Encode(encoder, pvc)
		if e != nil {
			klog.Error(e)
			return &utils.Response{Code: code.EncodeError, Msg: e.Error()}
		}
		klog.Info(d)
		return &utils.Response{Code: code.Success, Msg: "Success", Data: string(d)}
	}

	return &utils.Response{Code: code.Success, Msg: "Success", Data: pvc}
}
