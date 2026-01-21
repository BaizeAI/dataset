package modelscope

//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6 -generate

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
)

type HubAPIBaseResponse[T any] struct {
	Code      int64  `json:"Code"`
	Data      *T     `json:"Data,omitempty"`
	Message   string `json:"Message"`
	RequestID string `json:"RequestId"`
	Success   bool   `json:"Success"`
}

type HubAPILoginResponse struct {
	AccessToken string `json:"AccessToken"`
	Email       string `json:"Email"`
	Username    string `json:"Username"`
	WorkNo      string `json:"WorkNo"`
}

type HubAPIError struct {
	HubAPIBaseResponse[any]
}

func (e *HubAPIError) Error() string {
	return e.Message
}

func IsHubAPIError(err error) bool {
	_, ok := err.(*HubAPIError)
	return ok
}

const (
	HubAPIEndpointScheme = "https://"
	HubAPIEndpointDomain = "www.modelscope.cn"

	hubAPIEndpointPathLogin = "/api/v1/login"
)

//counterfeiter:generate -o fake/hub.go --fake-name FakeHubAPI . HubAPI
type HubAPI interface {
	Login(ctx context.Context, token string) (*HubAPIBaseResponse[HubAPILoginResponse], error)
}

type HubAPIClient struct {
	client      *http.Client
	apiEndpoint string
}

// NewHubAPIClient creates a new HubAPIClient.
//
// Source code: https://github.com/modelscope/modelscope/blob/058df0e34c8dad07659f326e71ffa68c133c4ec8/modelscope/hub/api.py#L62-L94
func NewHubAPIClient() *HubAPIClient {
	return &HubAPIClient{
		client: &http.Client{},
	}
}

func (c *HubAPIClient) endpoint() string {
	if c.apiEndpoint == "" {
		return HubAPIEndpointScheme + HubAPIEndpointDomain
	}

	return c.apiEndpoint
}

// Login signs in the user with the given token.
//
// The token is used to authenticate the user.
//
// Source code: https://github.com/modelscope/modelscope/blob/058df0e34c8dad07659f326e71ffa68c133c4ec8/modelscope/hub/api.py#L96-L133
func (c *HubAPIClient) Login(ctx context.Context, token string) (*HubAPIBaseResponse[HubAPILoginResponse], error) {
	body := struct {
		AccessToken string `json:"AccessToken"`
	}{
		AccessToken: token,
	}

	buffer := new(bytes.Buffer)
	err := json.NewEncoder(buffer).Encode(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, c.endpoint()+hubAPIEndpointPathLogin, buffer)
	if err != nil {
		return nil, err
	}

	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	var response HubAPIBaseResponse[HubAPILoginResponse]
	err = json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		return nil, err
	}
	if !response.Success {
		return nil, &HubAPIError{HubAPIBaseResponse: HubAPIBaseResponse[any]{
			Code:      response.Code,
			Message:   response.Message,
			RequestID: response.RequestID,
			Success:   response.Success,
		}}
	}

	return &response, nil
}
