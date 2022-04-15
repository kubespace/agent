package core

import (
	"github.com/openspacee/ospagent/pkg/config"
	"github.com/openspacee/ospagent/pkg/container"
	"github.com/openspacee/ospagent/pkg/kubernetes"
	"github.com/openspacee/ospagent/pkg/ospserver"
	"github.com/openspacee/ospagent/pkg/utils"
	"github.com/openspacee/ospagent/pkg/websocket"
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
	agentConfig.WebSocket = websocket.NewWebSocket(
		serverUrl,
		opt.AgentToken,
		agentConfig.RequestChan,
		agentConfig.ResponseChan,
		serverRespUrl)
	serverHttpsUrl := &url.URL{Scheme: "https", Host: opt.ServerUrl}
	ospServer, err := ospserver.NewOspServer(serverHttpsUrl)
	if err != nil {
		klog.Errorf("new ospserver error: %v", err)
		return nil, err
	}

	kubeClient := kubernetes.NewKubeClient(opt.KubeConfigFile)
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
