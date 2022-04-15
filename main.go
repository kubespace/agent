package main

import (
	"flag"
	"github.com/openspacee/ospagent/pkg/config"
	"github.com/openspacee/ospagent/pkg/core"
	"k8s.io/klog"
)

var (
	kubeConfigFile = flag.String("kubeconfig", "", "Path to kubeconfig file with authorization and master location information.")
	agentToken     = flag.String("token", "", "Agent token to connect to server.")
	serverUrl      = flag.String("server-url", "", "Server url agent to connect.")
)

func createAgentOptions() *config.AgentOptions {
	return &config.AgentOptions{
		KubeConfigFile: *kubeConfigFile,
		AgentToken:     *agentToken,
		ServerUrl:      *serverUrl,
	}
}

func buildAgent() (*core.Agent, error) {
	options := createAgentOptions()
	agentConfig, err := core.NewAgentConfig(options)
	if err != nil {
		klog.Error("New agent config error:", err)
		return nil, err
	}
	return core.NewAgent(agentConfig), nil
}

func main() {
	klog.InitFlags(nil)
	flag.Parse()
	flag.VisitAll(func(flag *flag.Flag) {
		klog.Infof("FLAG: --%s=%q", flag.Name, flag.Value)
	})
	agent, err := buildAgent()
	if err != nil {
		panic(err)
	}
	agent.Run()
}
