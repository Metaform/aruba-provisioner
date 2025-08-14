package api

import (
	"net/http"
)

type ManagementApi interface {
	CreateAsset(body string) (string, error)
	CreatePolicy(body string) (string, error)
	CreateContractDefinition(body string) (string, error)
}

type ManagementApiClient struct {
	HttpClient http.Client
	BaseUrl    string
	ApiKey     string
}

func (m *ManagementApiClient) CreateAsset(body string) (string, error) {
	return sendRequest(m.HttpClient, m.ApiKey, body, m.BaseUrl+"/assets")
}
func (m *ManagementApiClient) CreatePolicy(body string) (string, error) {
	return sendRequest(m.HttpClient, m.ApiKey, body, m.BaseUrl+"/policydefinitions")
}

func (m *ManagementApiClient) CreateContractDefinition(body string) (string, error) {
	url := m.BaseUrl + "/contractdefinitions"
	return sendRequest(m.HttpClient, m.ApiKey, body, url)
}
