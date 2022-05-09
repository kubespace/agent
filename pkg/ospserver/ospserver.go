package ospserver

import (
	"bytes"
	"fmt"
	"github.com/kubespace/agent/pkg/utils"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
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

func (o *OspServer) GetAppChart(chartPath string) (*chart.Chart, error) {
	ret, err := o.httpClient.Get(fmt.Sprintf("/app/charts/%s", chartPath), nil)
	if err != nil {
		return nil, err
	}
	charts, err := loader.LoadArchive(bytes.NewReader(ret))
	if err != nil {
		return nil, err
	}
	return charts, nil
}

func (o *OspServer) GetAgentYaml(token string) (string, error) {
	ret, err := o.httpClient.Get(fmt.Sprintf("/v1/import/%s", token), nil)
	if err != nil {
		return "", err
	}
	return string(ret), nil
}
