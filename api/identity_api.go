package api

import (
	"net/http"

	"k8s.io/apimachinery/pkg/util/json"
)

type IdentityApi interface {
	CreateParticipant(body string) (string, error)
}

type IdentityApiClient struct {
	HttpClient http.Client
	BaseUrl    string
	ApiKey     string
}

type ParticipantResponse struct {
	ClientId     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	ApiKey       string `json:"apiKey"`
}

func (i *IdentityApiClient) CreateParticipant(body string) (*ParticipantResponse, error) {
	jsonBody, err := sendRequest(i.HttpClient, i.ApiKey, body, i.BaseUrl+"/participants")

	if err != nil {
		return nil, err
	}

	var p ParticipantResponse
	err = json.Unmarshal([]byte(jsonBody), &p)
	return &p, err
}
