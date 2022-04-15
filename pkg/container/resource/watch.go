package resource

import (
	"encoding/json"
	"github.com/openspacee/ospagent/pkg/utils"
	"github.com/openspacee/ospagent/pkg/utils/code"
	"github.com/openspacee/ospagent/pkg/websocket"
	"k8s.io/klog"
	"sync"
)

type WatchResource struct {
	watch bool
	mutex sync.Mutex
	websocket.SendResponse
}

func NewWatchResource(sendResponse websocket.SendResponse) *WatchResource {
	return &WatchResource{
		watch:        false,
		mutex:        sync.Mutex{},
		SendResponse: sendResponse,
	}
}

type WatchParams struct {
	Action string `json:"action"`
}

func (w *WatchResource) WatchAction(requestParams interface{}) *utils.Response {
	params := &WatchParams{}
	json.Unmarshal(requestParams.([]byte), params)
	if params.Action == "" {
		return &utils.Response{Code: code.ParamsError, Msg: "Action param is blank"}
	}
	if params.Action != "close" && params.Action != "open" {
		return &utils.Response{Code: code.ParamsError, Msg: "Action param is not valid"}
	}
	klog.Infof("watch resource %s", params.Action)

	w.mutex.Lock()
	defer w.mutex.Unlock()

	if params.Action == "open" && !w.watch {
		w.watch = true
	}

	if params.Action == "close" && w.watch {
		w.watch = false
	}
	return &utils.Response{Code: code.Success, Msg: "Action watch resource success"}
}

func (w *WatchResource) WatchAdd(watchRes string) func(interface{}) {
	return func(obj interface{}) {
		if w.watch {
			resp := &utils.WatchResponse{Event: utils.AddEvent, Resource: obj, Obj: watchRes}
			w.SendResponse(resp, "", utils.WatchType)
		}
	}
}

func (w *WatchResource) WatchUpdate(watchRes string) func(interface{}, interface{}) {
	return func(oldObj, newObj interface{}) {
		if w.watch {
			resp := &utils.WatchResponse{Event: utils.UpdateEvent, Resource: newObj, Obj: watchRes}
			w.SendResponse(resp, "", utils.WatchType)
		}
	}
}

func (w *WatchResource) WatchDelete(watchRes string) func(interface{}) {
	return func(obj interface{}) {
		if w.watch {
			resp := &utils.WatchResponse{Event: utils.DeleteEvent, Resource: obj, Obj: watchRes}
			w.SendResponse(resp, "", utils.WatchType)
		}
	}
}
