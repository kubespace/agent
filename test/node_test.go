package test

import (
	"fmt"
	"github.com/kubespace/agent/pkg/container/resource"
	"github.com/kubespace/agent/pkg/kubernetes"
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
