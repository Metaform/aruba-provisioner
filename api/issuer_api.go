package api

type IssuerApi interface {
	CreateHolder(did string, holderId string, name string) (string, error)
}

func (i *ApiClient) CreateHolder(did string, holderId string, name string) error {
	url := i.BaseUrl + "/holders"

	body := `{
				"did": "` + did + `",
    			"holderId": "` + holderId + `",
 				"name": "` + name + `"
			}`
	_, err := sendRequest(i.HttpClient, i.ApiKey, body, url)
	return err
}
