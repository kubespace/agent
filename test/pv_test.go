package test

import (
	"fmt"
	"github.com/kubespace/agent/pkg/container/resource"
	"github.com/kubespace/agent/pkg/kubernetes"
	"testing"
)

func TestPV(t *testing.T) {
	kubeClient := kubernetes.NewKubeClient("../kubeconfig")

	pv := resource.PersistentVolume{
		KubeClient:   kubeClient,
		SendResponse: nil,
	}

	res := pv.List(nil)
	fmt.Println(res.Data)
}

func TestConfigMap(t *testing.T) {
	kubeClient := kubernetes.NewKubeClient("../kubeconfig")

	configMap := resource.ConfigMap{
		KubeClient:   kubeClient,
		SendResponse: nil,
	}

	res := configMap.List(nil)
	fmt.Println(res.Data)
}
