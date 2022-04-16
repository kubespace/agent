package resource

import (
	"encoding/json"
	"fmt"
	"github.com/kubespace/agent/pkg/kubernetes"
	"github.com/kubespace/agent/pkg/utils"
	"github.com/kubespace/agent/pkg/utils/code"
	"k8s.io/api/batch/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog"
	"strconv"
	"strings"
)

var CronJobGVR = &schema.GroupVersionResource{
	Group:    "batch",
	Version:  "v1beta1",
	Resource: "cronjobs",
}

type CronJob struct {
	watch *WatchResource
	*DynamicResource
}

func NewCronJob(kubeClient *kubernetes.KubeClient, watch *WatchResource) *CronJob {
	d := &CronJob{
		watch:           watch,
		DynamicResource: NewDynamicResource(kubeClient, CronJobGVR),
	}
	d.DoWatch()
	return d
}

func (c *CronJob) DoWatch() {
	informer := c.KubeClient.CronJobInformer().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.watch.WatchAdd(utils.WatchCronjob),
		UpdateFunc: c.watch.WatchUpdate(utils.WatchCronjob),
		DeleteFunc: c.watch.WatchDelete(utils.WatchCronjob),
	})
}

type BuildCronJob struct {
	UID               string                   `json:"uid"`
	Name              string                   `json:"name"`
	Namespace         string                   `json:"namespace"`
	Active            []corev1.ObjectReference `json:"active"`
	LastScheduleTime  *metav1.Time             `json:"last_schedule_time"`
	Schedule          string                   `json:"schedule"`
	ConcurrencyPolicy string                   `json:"concurrency_policy"`
	ResourceVersion   string                   `json:"resource_version"`
	Suspend           string                   `json:"suspend"`
	Created           metav1.Time              `json:"created"`
}

func (c *CronJob) ToBuildCronJob(cronjob *v1beta1.CronJob) *BuildCronJob {
	if cronjob == nil {
		return nil
	}
	data := &BuildCronJob{
		UID:               string(cronjob.UID),
		Name:              cronjob.Name,
		Namespace:         cronjob.Namespace,
		Active:            cronjob.Status.Active,
		LastScheduleTime:  cronjob.Status.LastScheduleTime,
		Schedule:          cronjob.Spec.Schedule,
		ConcurrencyPolicy: string(cronjob.Spec.ConcurrencyPolicy),
		Suspend:           strconv.FormatBool(*cronjob.Spec.Suspend),
		Created:           cronjob.CreationTimestamp,
	}

	return data
}

type CronJobQueryParams struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	UID       string `json:"uid"`
	Output    string `json:"output"`
}

type CronJobUpdateParams struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Replicas  int32  `json:"replicas"`
}

func (c *CronJob) List(requestParams interface{}) *utils.Response {
	queryParams := &CronJobQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	list, err := c.KubeClient.InformerRegistry.CronJobInformer().Lister().List(labels.Everything())
	if err != nil {
		return &utils.Response{
			Code: code.ListError,
			Msg:  err.Error(),
		}
	}
	var cronjobs []*BuildCronJob
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
		cronjobs = append(cronjobs, c.ToBuildCronJob(ds))
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: cronjobs}
}

func (c *CronJob) Get(requestParams interface{}) *utils.Response {
	queryParams := &CronJobQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	if queryParams.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "CronJob name is blank"}
	}
	if queryParams.Namespace == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Namespace is blank"}
	}
	cronjob, err := c.KubeClient.InformerRegistry.CronJobInformer().Lister().CronJobs(queryParams.Namespace).Get(queryParams.Name)
	if err != nil {
		return &utils.Response{Code: code.GetError, Msg: err.Error()}
	}
	if queryParams.Output == "yaml" {
		const mediaType = runtime.ContentTypeYAML
		rscheme := runtime.NewScheme()
		v1beta1.AddToScheme(rscheme)
		codecs := serializer.NewCodecFactory(rscheme)
		info, ok := runtime.SerializerInfoForMediaType(codecs.SupportedMediaTypes(), mediaType)
		if !ok {
			return &utils.Response{Code: code.Success, Msg: fmt.Sprintf("unsupported media type %q", mediaType)}
		}

		encoder := codecs.EncoderForVersion(info.Serializer, c.GroupVersion())
		//klog.Info(a)
		d, e := runtime.Encode(encoder, cronjob)
		if e != nil {
			klog.Error(e)
			return &utils.Response{Code: code.Success, Msg: e.Error()}
		}
		return &utils.Response{Code: code.Success, Msg: "Success", Data: string(d)}
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: cronjob}
}

func (c *CronJob) UpdateObj(updateParams interface{}) *utils.Response {
	params := &CronJobUpdateParams{}
	json.Unmarshal(updateParams.([]byte), params)
	if params.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "CronJob name is blank"}
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
		result, getErr := c.KubeClient.InformerRegistry.CronJobInformer().Lister().CronJobs(params.Namespace).Get(params.Name)
		if getErr != nil {
			panic(fmt.Errorf("failed to get latest version of CronJob: %v", getErr))
		}

		//result.Spec.Replicas = &params.Replicas
		_, updateErr := c.ClientSet.BatchV1beta1().CronJobs(params.Namespace).Update(c.context, result, metav1.UpdateOptions{})
		return updateErr
	})
	if retryErr != nil {
		klog.Errorf("Update failed: %v", retryErr)
		return &utils.Response{Code: code.ParamsError, Msg: retryErr.Error()}
	}
	return &utils.Response{Code: code.Success, Msg: "Success"}
}
