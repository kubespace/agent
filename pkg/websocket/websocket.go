package websocket

import (
	"crypto/tls"
	"encoding/json"
	"github.com/gorilla/websocket"
	"github.com/kubespace/agent/pkg/container/resource"
	"github.com/kubespace/agent/pkg/ospserver"
	"github.com/kubespace/agent/pkg/utils"
	"github.com/kubespace/agent/pkg/utils/code"
	"k8s.io/klog"
	"net/http"
	"net/url"
	"time"
)

const (
	CloseExecConn = "closeExecConn"
)

type ExecCloseParams struct {
	SessionId string `json:"session_id"`
}

type SendResponse func(interface{}, string, string)

type ExecWebSocket struct {
	ResponseChan chan *utils.TResponse
	Conn         *websocket.Conn
	StopChan     chan struct{}
}

type WebSocket struct {
	Url              *url.URL
	RespUrl          *url.URL
	Token            string
	RequestChan      chan *utils.Request
	ResponseChan     chan *utils.TResponse
	ExecResponseChan chan *utils.TResponse
	Conn             *websocket.Conn
	ExecResponseMap  map[string]*ExecWebSocket
	OspServer        *ospserver.OspServer
	DynamicResource  *resource.DynamicResource
}

func NewWebSocket(
	url *url.URL,
	token string,
	requestChan chan *utils.Request,
	responseChan chan *utils.TResponse,
	respUrl *url.URL,
	ospServer *ospserver.OspServer,
	dynRes *resource.DynamicResource) *WebSocket {
	return &WebSocket{
		Url:              url,
		Token:            token,
		RequestChan:      requestChan,
		ResponseChan:     responseChan,
		ExecResponseChan: make(chan *utils.TResponse, 1000),
		RespUrl:          respUrl,
		ExecResponseMap:  make(map[string]*ExecWebSocket),
		OspServer:        ospServer,
		DynamicResource:  dynRes,
	}
}

func (ws *WebSocket) ReadRequest() {
	defer ws.Conn.Close()

	ws.reconnectServer()
	for {
		_, data, err := ws.Conn.ReadMessage()
		if err != nil {
			klog.Error("read err:", err)
			ws.Conn.Close()
			ws.reconnectServer()
			continue
		}
		klog.V(1).Infof("request data: %s", string(data))
		request := &utils.Request{}
		err = json.Unmarshal(data, request)
		if err != nil {
			klog.Errorf("unserializer request data error: %s", err)
		} else {
			if request.Action == CloseExecConn {
				go ws.handleCloseExecConn(request)
			} else {
				ws.RequestChan <- request
			}
		}
	}
}

func (ws *WebSocket) handleCloseExecConn(request *utils.Request) {
	params := &ExecCloseParams{}
	requestParams, _ := json.Marshal(request.Params)
	err := json.Unmarshal(requestParams, params)
	res := &utils.Response{Code: code.Success}

	if err != nil {
		klog.Errorf("unserializer request data error: %s", err)
		res.Code = code.ParamsError
		res.Msg = err.Error()
	} else {
		sessionId := params.SessionId
		if sessionId != "" {
			execResp, _ := ws.ExecResponseMap[sessionId]
			if execResp != nil {
				close(execResp.StopChan)
			} else {
				klog.Errorf("not found session %s get exec response", sessionId)
				res.Code = code.ParamsError
				res.Msg = "not found exec response"
			}
			klog.V(1).Infof("close exec session %s success", sessionId)
		} else {
			klog.Errorf("request %s not found param session id", request.RequestId)
			res.Code = code.ParamsError
			res.Msg = "params not found session id"
		}
	}

	response := &utils.TResponse{
		ResType:   utils.RequestType,
		RequestId: request.RequestId,
		Data:      res,
	}
	ws.ResponseChan <- response
}

func (ws *WebSocket) reconnectServer() {
	err := ws.connectServer()
	if err == nil {
		return
	}
	ticker := time.NewTicker(time.Second * 5)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			err := ws.connectServer()
			if err == nil {
				return
			}
		}
	}
}

func (ws *WebSocket) connectServer() error {
	klog.Info("start connect to server ", ws.Url.String())
	wsHeader := http.Header{}
	wsHeader.Add("token", ws.Token)
	d := &websocket.Dialer{TLSClientConfig: &tls.Config{RootCAs: nil, InsecureSkipVerify: true}}
	conn, _, err := d.Dial(ws.Url.String(), wsHeader)
	if err != nil {
		klog.Infof("connect to server %s error: %v, retry after 5 seconds\n", ws.Url.String(), err)
		return err
	} else {
		klog.Infof("connect to server %s success\n", ws.Url.String())
		ws.Conn = conn
		ws.updateAgent()
		return nil
	}
}

func (ws *WebSocket) updateAgent() {
	agentYaml, err := ws.OspServer.GetAgentYaml(ws.Token)
	if err != nil {
		klog.Errorf("get agent yaml error: %s", err)
		return
	}
	resp := ws.DynamicResource.ApplyYaml(agentYaml)
	if !resp.IsSuccess() {
		klog.Errorf("apply agent yaml error: %s", resp.Msg)
	}
}

func (ws *WebSocket) WriteResponse() {
	for {
		select {
		case resp, ok := <-ws.ResponseChan:
			if ok {
				if resp.ResType == utils.ExecType {
					// 发送到缓存，不阻塞
					ws.ExecResponseChan <- resp
				} else {
					// 并发发送消息
					go ws.doSendResponse(resp)
				}
			}
		}
	}
}

func (ws *WebSocket) WriteExecResponse() {
	for {
		select {
		case resp, ok := <-ws.ExecResponseChan:
			if ok {
				//klog.Info(resp)
				ws.doSendExecResponse(resp)
			}
		}
	}
}

func (ws *WebSocket) doSendResponse(resp *utils.TResponse) {
	respMsg, err := resp.Serializer()
	if err != nil {
		klog.Errorf("response %v serializer error: %s", resp, err)
		return
	}

	klog.V(1).Info("start connect to server response websocket", ws.Url.String())
	wsHeader := http.Header{}
	wsHeader.Add("token", ws.Token)

	d := &websocket.Dialer{TLSClientConfig: &tls.Config{RootCAs: nil, InsecureSkipVerify: true}}
	conn, _, err := d.Dial(ws.RespUrl.String(), wsHeader)
	defer func() {
		if conn != nil {
			conn.Close()
		}
	}()

	if err != nil {
		klog.Errorf("connect to server %s error: %v", ws.RespUrl.String(), err)
	} else {
		conn.WriteMessage(websocket.TextMessage, respMsg)
		klog.V(1).Infof("write response %s success", string(respMsg))
	}
}

func (ws *WebSocket) doSendExecResponse(resp *utils.TResponse) {
	execResp, ok := ws.ExecResponseMap[resp.RequestId]
	if !ok {
		wsHeader := http.Header{}
		wsHeader.Add("token", ws.Token)

		d := &websocket.Dialer{TLSClientConfig: &tls.Config{RootCAs: nil, InsecureSkipVerify: true}}
		execWs, _, err := d.Dial(ws.RespUrl.String(), wsHeader)
		if err != nil {
			klog.Errorf("connect to server %s error: %v", ws.RespUrl.String(), err)
			return
		} else {
			execResp = &ExecWebSocket{
				Conn:         execWs,
				ResponseChan: make(chan *utils.TResponse),
				StopChan:     make(chan struct{}),
			}
			ws.ExecResponseMap[resp.RequestId] = execResp
			go func() {
				defer func() {
					if execResp.Conn != nil {
						execResp.Conn.Close()
					}
					delete(ws.ExecResponseMap, resp.RequestId)
				}()
				execStop := false
				for {
					select {
					case resp, ok := <-execResp.ResponseChan:
						if ok {
							respMsg, err := resp.Serializer()
							//klog.Info(string(respMsg))
							if err != nil {
								klog.Errorf("response %v serializer error: %s", resp, err)
								return
							}
							execResp.Conn.WriteMessage(websocket.TextMessage, respMsg)
						}
					case <-execResp.StopChan:
						execStop = true
						break
					}
					if execStop {
						break
					}
				}
				klog.V(1).Infof("exec response session %s finish", resp.RequestId)
			}()
		}
	}
	execResp.ResponseChan <- resp
}

func (ws *WebSocket) SendResponse(resp interface{}, requestId, resType string) {
	if ws.Conn != nil {
		tResp := &utils.TResponse{RequestId: requestId, Data: resp, ResType: resType}
		ws.ResponseChan <- tResp
	}
}
