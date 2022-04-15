package resource

import (
	"encoding/json"
	"fmt"
	"github.com/kubespace/agent/pkg/kubernetes"
	"github.com/kubespace/agent/pkg/utils"
	"github.com/kubespace/agent/pkg/utils/code"
	"k8s.io/api/batch/v1"
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

type Job struct {
	watch *WatchResource
	*DynamicResource
}

func NewJob(kubeClient *kubernetes.KubeClient, watch *WatchResource) *Job {
	d := &Job{
		watch: watch,
		DynamicResource: NewDynamicResource(kubeClient, &schema.GroupVersionResource{
			Group:    "batch",
			Version:  "v1",
			Resource: "jobs",
		}),
	}
	d.DoWatch()
	return d
}

func (j *Job) DoWatch() {
	informer := j.KubeClient.JobInformer().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    j.watch.WatchAdd(utils.WatchJob),
		UpdateFunc: j.watch.WatchUpdate(utils.WatchJob),
		DeleteFunc: j.watch.WatchDelete(utils.WatchJob),
	})
}

type BuildJob struct {
	UID             string            `json:"uid"`
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace"`
	Completions     *int32            `json:"completions"`
	Active          int32             `json:"active"`
	Succeeded       int32             `json:"succeeded"`
	Failed          int32             `json:"failed"`
	ResourceVersion string            `json:"resource_version"`
	Conditions      []string          `json:"conditions"`
	NodeSelector    map[string]string `json:"node_selector"`
	Created         metav1.Time       `json:"created"`
}

func (j *Job) ToBuildJob(job *v1.Job) *BuildJob {
	if job == nil {
		return nil
	}
	var conditions []string
	for _, c := range job.Status.Conditions {
		if c.Status == corev1.ConditionTrue {
			conditions = append(conditions, string(c.Type))
		}
	}
	data := &BuildJob{
		UID:          string(job.UID),
		Name:         job.Name,
		Namespace:    job.Namespace,
		Completions:  job.Spec.Completions,
		Active:       job.Status.Active,
		Succeeded:    job.Status.Succeeded,
		Failed:       job.Status.Failed,
		Conditions:   conditions,
		NodeSelector: job.Spec.Template.Spec.NodeSelector,
		Created:      job.CreationTimestamp,
	}

	return data
}

type JobQueryParams struct {
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
	UID        string `json:"uid"`
	Output     string `json:"output"`
	CronJobUID string `json:"cronjob_uid"`
}

type JobUpdateParams struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Replicas  int32  `json:"replicas"`
}

func (j *Job) List(requestParams interface{}) *utils.Response {
	queryParams := &JobQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	list, err := j.KubeClient.InformerRegistry.JobInformer().Lister().List(labels.Everything())
	if err != nil {
		return &utils.Response{
			Code: code.ListError,
			Msg:  err.Error(),
		}
	}
	var jobs []*BuildJob
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
		if queryParams.CronJobUID != "" {
			cronFind := false
			for _, ref := range ds.OwnerReferences {
				if string(ref.UID) == queryParams.CronJobUID && ref.Kind == "CronJob" {
					cronFind = true
					break
				}
			}
			if !cronFind {
				continue
			}
		}
		jobs = append(jobs, j.ToBuildJob(ds))
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: jobs}
}

func (j *Job) Get(requestParams interface{}) *utils.Response {
	queryParams := &JobQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	if queryParams.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Job name is blank"}
	}
	if queryParams.Namespace == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Namespace is blank"}
	}
	job, err := j.KubeClient.InformerRegistry.JobInformer().Lister().Jobs(queryParams.Namespace).Get(queryParams.Name)
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

		encoder := codecs.EncoderForVersion(info.Serializer, j.GroupVersion())
		//klog.Info(a)
		d, e := runtime.Encode(encoder, job)
		if e != nil {
			klog.Error(e)
			return &utils.Response{Code: code.Success, Msg: e.Error()}
		}
		return &utils.Response{Code: code.Success, Msg: "Success", Data: string(d)}
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: job}
}

func (j *Job) UpdateObj(updateParams interface{}) *utils.Response {
	params := &JobUpdateParams{}
	json.Unmarshal(updateParams.([]byte), params)
	if params.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Job name is blank"}
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
		result, getErr := j.KubeClient.InformerRegistry.JobInformer().Lister().Jobs(params.Namespace).Get(params.Name)
		if getErr != nil {
			panic(fmt.Errorf("failed to get latest version of Job: %v", getErr))
		}

		//result.Spec.Replicas = &params.Replicas
		_, updateErr := j.ClientSet.BatchV1().Jobs(params.Namespace).Update(j.context, result, metav1.UpdateOptions{})
		return updateErr
	})
	if retryErr != nil {
		klog.Errorf("Update failed: %v", retryErr)
		return &utils.Response{Code: code.ParamsError, Msg: retryErr.Error()}
	}
	return &utils.Response{Code: code.Success, Msg: "Success"}
}
