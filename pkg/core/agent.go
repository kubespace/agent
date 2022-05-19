package core

import (
	"github.com/kubespace/agent/pkg/config"
	"github.com/kubespace/agent/pkg/container"
	"github.com/kubespace/agent/pkg/container/resource"
	"github.com/kubespace/agent/pkg/kubernetes"
	"github.com/kubespace/agent/pkg/ospserver"
	"github.com/kubespace/agent/pkg/utils"
	"github.com/kubespace/agent/pkg/websocket"
	"k8s.io/klog"
	"net/url"
)

type AgentConfig struct {
	AgentOptions *config.AgentOptions
	Container    *container.Container
	WebSocket    *websocket.WebSocket
	RequestChan  chan *utils.Request
	ResponseChan chan *utils.TResponse
}

func NewAgentConfig(opt *config.AgentOptions) (*AgentConfig, error) {
	agentConfig := &AgentConfig{
		AgentOptions: opt,
		RequestChan:  make(chan *utils.Request),
		ResponseChan: make(chan *utils.TResponse),
	}

	serverUrl := &url.URL{Scheme: "ws", Host: opt.ServerUrl, Path: "/api/v1/kube/connect"}
	serverRespUrl := &url.URL{Scheme: "ws", Host: opt.ServerUrl, Path: "/api/v1/kube/response"}
	serverHttpsUrl := &url.URL{Scheme: "http", Host: opt.ServerUrl}
	ospServer, err := ospserver.NewOspServer(serverHttpsUrl)
	if err != nil {
		klog.Errorf("new ospserver error: %v", err)
		return nil, err
	}

	kubeClient := kubernetes.NewKubeClient(opt.KubeConfigFile)
	dynamicResource := resource.NewDynamicResource(kubeClient, nil)
	updateAgent := true
	//if opt.KubeConfigFile != "" {
	//	updateAgent = false
	//}
	agentConfig.WebSocket = websocket.NewWebSocket(
		serverUrl,
		opt.AgentToken,
		agentConfig.RequestChan,
		agentConfig.ResponseChan,
		serverRespUrl,
		ospServer,
		dynamicResource,
		updateAgent)
	agentConfig.Container = container.NewContainer(
		//nil,
		kubeClient,
		agentConfig.RequestChan,
		agentConfig.ResponseChan,
		agentConfig.WebSocket.SendResponse,
		ospServer)

	return agentConfig, nil
}

type Agent struct {
	Container *container.Container
	WebSocket *websocket.WebSocket
}

func NewAgent(config *AgentConfig) *Agent {
	return &Agent{
		Container: config.Container,
		WebSocket: config.WebSocket,
	}
}

func (a *Agent) Run() {
	go a.WebSocket.ReadRequest()
	go a.WebSocket.WriteResponse()
	go a.WebSocket.WriteExecResponse()
	a.Container.Run()
}
