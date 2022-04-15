package kubernetes

import (
	apiExtensionsClientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	utilversion "k8s.io/apimachinery/pkg/util/version"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	kube_client "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
)

type KubeClient struct {
	KubeConfigFile string
	ClientSet      kube_client.Interface
	DynamicClient  dynamic.Interface
	Config         *rest.Config
	//ListRegistry
	InformerRegistry
	*discovery.DiscoveryClient
	ApiExtensionsClientSet apiExtensionsClientset.Interface
	Version                *version.Info
}

func getKubeConfig(kubeConfigFile string) *rest.Config {
	if kubeConfigFile != "" {
		klog.Infof("using kubeconfig file: %s", kubeConfigFile)
		// use the current context in kubeconfig
		config, err := clientcmd.BuildConfigFromFlags("", kubeConfigFile)
		if err != nil {
			panic("failed to build config: " + err.Error())
		}
		return config
	}

	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}

	return kubeConfig
}

func NewKubeClient(kubeConfigFile string) *KubeClient {
	config := getKubeConfig(kubeConfigFile)
	kubeClient := kube_client.NewForConfigOrDie(config)
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	//listRegistry := NewListRegistry(kubeClient, nil)
	informerRegistry, err := NewInformerRegistry(kubeClient, nil)
	if err != nil {
		panic(err.Error())
	}
	dc, err := discovery.NewDiscoveryClientForConfig(config)
	apiExtensions, err := apiExtensionsClientset.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	ver, err := kubeClient.ServerVersion()
	return &KubeClient{
		KubeConfigFile: kubeConfigFile,
		ClientSet:      kubeClient,
		DynamicClient:  dynamicClient,
		Config:         config,
		//ListRegistry:     listRegistry,
		InformerRegistry:       informerRegistry,
		DiscoveryClient:        dc,
		ApiExtensionsClientSet: apiExtensions,
		Version:                ver,
	}
}

func VersionGreaterThan19(ver *version.Info) bool {
	if utilversion.MustParseSemantic(ver.GitVersion).LessThan(utilversion.MustParseSemantic("v1.19.0")) {
		return false
	}
	return true
}
