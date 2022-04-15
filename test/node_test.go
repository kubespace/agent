package test

import (
	"fmt"
	"github.com/openspacee/ospagent/pkg/container/resource"
	"github.com/openspacee/ospagent/pkg/kubernetes"
	"testing"
)

func TestNode(t *testing.T) {
	kubeClient := kubernetes.NewKubeClient("../kubeconfig")

	node := resource.Node{
		KubeClient:   kubeClient,
		SendResponse: nil,
	}

	res := node.List(nil)
	fmt.Println(res.Data)
}
