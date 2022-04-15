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
)

type PersistentVolume struct {
	*kubernetes.KubeClient
	watch *WatchResource
	*DynamicResource
}

type BuildPersistentVolume struct {
	UID            string                           `json:"uid"`
	Name           string                           `json:"name"`
	Status         string                           `json:"status"`
	Claim          string                           `json:"claim"`
	ClaimNamespace string                           `json:"claim_namespace"`
	StorageClass   string                           `json:"storage_class"`
	Capacity       string                           `json:"capacity"`
	AccessModes    []v1.PersistentVolumeAccessMode  `json:"access_modes"`
	CreateTime     metav1.Time                      `json:"create_time"`
	ReclaimPolicy  v1.PersistentVolumeReclaimPolicy `json:"reclaim_policy"`
}

func NewPersistentVolume(kubeClient *kubernetes.KubeClient, watch *WatchResource) *PersistentVolume {
	pv := &PersistentVolume{
		KubeClient: kubeClient,
		watch:      watch,
		DynamicResource: NewDynamicResource(kubeClient, &schema.GroupVersionResource{
			Group:    "",
			Version:  "v1",
			Resource: "persistentvolumes",
		}),
	}
	pv.DoWatch()
	return pv
}

func (p *PersistentVolume) DoWatch() {
	informer := p.KubeClient.PersistentVolumeInformer().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    p.watch.WatchAdd(utils.WatchPv),
		UpdateFunc: p.watch.WatchUpdate(utils.WatchPv),
		DeleteFunc: p.watch.WatchDelete(utils.WatchPv),
	})
}

func (p *PersistentVolume) ToBuildPersistentVolume(pv *v1.PersistentVolume) *BuildPersistentVolume {
	if pv == nil {
		return nil
	}
	var volumeSize, claimName, claimNamespace string
	if size, ok := pv.Spec.Capacity["storage"]; !ok {
		volumeSize = ""
	} else {
		volumeSize = size.String()
	}
	if (pv.Spec.ClaimRef) != nil {
		claimName = pv.Spec.ClaimRef.Name
		claimNamespace = pv.Spec.ClaimRef.Namespace
	}
	pvData := &BuildPersistentVolume{
		UID:            string(pv.UID),
		Name:           pv.Name,
		Status:         string(pv.Status.Phase),
		StorageClass:   pv.Spec.StorageClassName,
		Capacity:       volumeSize,
		Claim:          claimName,
		ClaimNamespace: claimNamespace,
		AccessModes:    pv.Spec.AccessModes,
		ReclaimPolicy:  pv.Spec.PersistentVolumeReclaimPolicy,
		CreateTime:     pv.CreationTimestamp,
	}

	return pvData
}

func (p *PersistentVolume) List(requestParams interface{}) *utils.Response {
	persistentVolumeList, err := p.KubeClient.InformerRegistry.PersistentVolumeInformer().Lister().List(labels.Everything())
	if err != nil {
		return &utils.Response{
			Code: code.ListError,
			Msg:  err.Error(),
		}
	}
	var persistentVolumeResource []*BuildPersistentVolume
	for _, pv := range persistentVolumeList {
		persistentVolumeResource = append(persistentVolumeResource, p.ToBuildPersistentVolume(pv))
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: persistentVolumeResource}
}

func (p *PersistentVolume) Get(requestParams interface{}) *utils.Response {
	queryParams := &ConfigMapQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	if queryParams.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Name is blank"}
	}
	pv, err := p.KubeClient.PersistentVolumeInformer().Lister().Get(queryParams.Name)
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
		d, e := runtime.Encode(encoder, pv)
		if e != nil {
			klog.Error(e)
			return &utils.Response{Code: code.EncodeError, Msg: e.Error()}
		}
		return &utils.Response{Code: code.Success, Msg: "Success", Data: string(d)}
	}

	return &utils.Response{Code: code.Success, Msg: "Success", Data: pv}
}
