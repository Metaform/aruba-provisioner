package api

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

func sendRequest(client http.Client, apiKey string, body string, url string) (string, error) {
	payload := strings.NewReader(body)

	rq, err := http.NewRequest("POST", url, payload)
	if err != nil {
		return "", err
	}
	rq.Header.Add("Content-Type", "application/json")
	rq.Header.Add("x-api-key", apiKey)

	resp, err := client.Do(rq)
	if err != nil {
		return "", err
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			fmt.Printf("Error closing response body: %v\n", err)
		}
	}(resp.Body)

	response, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 409 && (resp.StatusCode < 200 || resp.StatusCode >= 300) {
		fmt.Println("Error sending request: ", resp.Status, " ", string(response))
		return "", fmt.Errorf("error sending request: %s", resp.Status)
	}
	return string(response), nil
}
