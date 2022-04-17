package main

import (
	"flag"
	"github.com/kubespace/agent/pkg/config"
	"github.com/kubespace/agent/pkg/core"
	"k8s.io/klog"
	"os"
)

var (
	kubeConfigFile = flag.String("kubeconfig", "", "Path to kubeconfig file with authorization and master location information.")
	agentToken     = flag.String("token", LookupEnvOrString("TOKEN", "local"), "Agent token to connect to server.")
	serverUrl      = flag.String("server-url", LookupEnvOrString("SERVER_URL", "kubespace"), "Server url agent to connect.")
)

func LookupEnvOrString(key string, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	return defaultVal
}

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
