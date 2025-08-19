package api

type ManagementApi interface {
	CreateAsset(body string) (string, error)
	CreatePolicy(body string) (string, error)
	CreateContractDefinition(body string) (string, error)
	CreateSecret(body string) (string, error)
}

func (i *ApiClient) CreateAsset(body string) (string, error) {
	return sendRequest(i.HttpClient, i.ApiKey, body, i.BaseUrl+"/assets")
}
func (i *ApiClient) CreatePolicy(body string) (string, error) {
	return sendRequest(i.HttpClient, i.ApiKey, body, i.BaseUrl+"/policydefinitions")
}

func (i *ApiClient) CreateContractDefinition(body string) (string, error) {
	url := i.BaseUrl + "/contractdefinitions"
	return sendRequest(i.HttpClient, i.ApiKey, body, url)
}

func (i *ApiClient) CreateSecret(body string) (string, error) {
	return sendRequest(i.HttpClient, i.ApiKey, body, i.BaseUrl+"/secrets")
}
