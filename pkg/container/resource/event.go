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

type Event struct {
	watch *WatchResource
	*DynamicResource
}

func NewEvent(kubeClient *kubernetes.KubeClient, watch *WatchResource) *Event {
	e := &Event{
		watch: watch,
		DynamicResource: NewDynamicResource(kubeClient, &schema.GroupVersionResource{
			Group:    "",
			Version:  "v1",
			Resource: "events",
		}),
	}
	e.DoWatch()
	return e
}

func (e *Event) DoWatch() {
	eventInformer := e.KubeClient.EventInformer().Informer()
	eventInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    e.watch.WatchAdd(utils.WatchEvent),
		UpdateFunc: e.watch.WatchUpdate(utils.WatchEvent),
		DeleteFunc: e.watch.WatchDelete(utils.WatchEvent),
	})
}

type BuildEvent struct {
	UID             string              `json:"uid"`
	Namespace       string              `json:"namespace"`
	Reason          string              `json:"reason"`
	Message         string              `json:"message"`
	Type            string              `json:"type"`
	Object          *v1.ObjectReference `json:"object"`
	Source          *v1.EventSource     `json:"source"`
	EventTime       metav1.Time         `json:"event_time"`
	Count           int32               `json:"count"`
	ResourceVersion string              `json:"resource_version"`
}

func (e *Event) ToBuildEvent(event *v1.Event) *BuildEvent {
	if e == nil {
		return nil
	}
	eventTime := event.LastTimestamp
	if eventTime.IsZero() {
		eventTime = event.FirstTimestamp
	}
	if eventTime.IsZero() {
		eventTime = event.CreationTimestamp
	}
	eventData := &BuildEvent{
		UID:             string(event.UID),
		Namespace:       event.Namespace,
		Reason:          event.Reason,
		Message:         event.Message,
		Type:            event.Type,
		Object:          &event.InvolvedObject,
		Source:          &event.Source,
		EventTime:       eventTime,
		Count:           event.Count,
		ResourceVersion: event.ResourceVersion,
	}

	return eventData
}

type EventQueryParams struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	UID       string `json:"uid"`
	Output    string `json:"output"`
}

func (e *Event) List(requestParams interface{}) *utils.Response {
	queryParams := &EventQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	eventList, err := e.KubeClient.InformerRegistry.EventInformer().Lister().List(labels.Everything())
	if err != nil {
		return &utils.Response{
			Code: code.ListError,
			Msg:  err.Error(),
		}
	}
	var events []*BuildEvent
	for _, event := range eventList {
		if queryParams.UID != "" && string(event.InvolvedObject.UID) != queryParams.UID {
			continue
		}
		if queryParams.Kind != "" && event.InvolvedObject.Kind != queryParams.Kind {
			continue
		}
		if queryParams.Namespace != "" && event.InvolvedObject.Namespace != queryParams.Namespace {
			continue
		}
		if queryParams.Name != "" && event.InvolvedObject.Name != queryParams.Name {
			continue
		}
		events = append(events, e.ToBuildEvent(event))
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: events}
}

func (e *Event) Get(requestParams interface{}) *utils.Response {
	queryParams := &EventQueryParams{}
	json.Unmarshal(requestParams.([]byte), queryParams)
	if queryParams.Name == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Event name is blank"}
	}
	if queryParams.Namespace == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Namespace is blank"}
	}
	event, err := e.KubeClient.InformerRegistry.EventInformer().Lister().Events(queryParams.Namespace).Get(queryParams.Name)
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

		encoder := codecs.EncoderForVersion(info.Serializer, e.GroupVersion())
		//klog.Info(a)
		d, e := runtime.Encode(encoder, event)
		if e != nil {
			klog.Error(e)
			return &utils.Response{Code: code.Success, Msg: e.Error()}
		}
		return &utils.Response{Code: code.Success, Msg: "Success", Data: string(d)}
	}
	return &utils.Response{Code: code.Success, Msg: "Success", Data: event}
}
