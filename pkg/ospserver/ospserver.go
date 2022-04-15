package ospserver

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/kubespace/agent/pkg/utils"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"k8s.io/klog"
	"net/url"
)

type OspServer struct {
	Url        *url.URL
	httpClient *utils.HttpClient
}

func NewOspServer(url *url.URL) (*OspServer, error) {
	client, err := utils.NewHttpClient(url.String())
	if err != nil {
		return nil, err
	}
	return &OspServer{
		Url:        url,
		httpClient: client,
	}, nil
}

func (o *OspServer) GetAppChart(name, chartVersion string) (*chart.Chart, error) {
	ret, err := o.httpClient.Get(fmt.Sprintf("/app/charts/%s/%s", name, chartVersion), nil)
	if err != nil {
		klog.Errorf("get app charts error: %s", err)
		return nil, err
	}
	res := &utils.Response{}
	err = json.Unmarshal(ret, res)
	if err != nil {
		klog.Error("unmarshal return app chart error: %s", err)
		return nil, err
	}
	if !res.IsSuccess() {
		return nil, fmt.Errorf("%s", res.Msg)
	}
	chartString := res.Data.(string)
	tarDecode, err := base64.StdEncoding.DecodeString(chartString)
	if err != nil {
		return nil, err
	}

	charts, err := loader.LoadArchive(bytes.NewReader(tarDecode))
	if err != nil {
		return nil, err
	}
	return charts, nil
}
