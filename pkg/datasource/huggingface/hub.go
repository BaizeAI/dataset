package huggingface

//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6 -generate

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"
)

const (
	HubAPIEndpointScheme = "https://"
	HubAPIEndpointDomain = "huggingface.co"

	hubAPIEndpointPathWhoAmI = "/api/whoami-v2"
)

type HfAPIAccessToken struct {
	CreatedAt   time.Time `json:"createdAt"`
	DisplayName string    `json:"displayName"`
	Role        string    `json:"role"`
}

type HfAPIWhoAmIResponseAuth struct {
	AccessToken HfAPIAccessToken `json:"accessToken"`
	Type        string           `json:"type"`
}

type HfAPIWhoAmIResponse struct {
	Auth          HfAPIWhoAmIResponseAuth `json:"auth"`
	AvatarURL     string                  `json:"avatarUrl"`
	CanPay        bool                    `json:"canPay"`
	Email         string                  `json:"email"`
	EmailVerified bool                    `json:"emailVerified"`
	Fullname      string                  `json:"fullname"`
	ID            string                  `json:"id"`
	IsPro         bool                    `json:"isPro"`
	Name          string                  `json:"name"`
	Type          string                  `json:"type"`
}

type HfAPIErrorResponse struct {
	Error string `json:"error"`
}

type HfAPIError struct {
	HfAPIErrorResponse
}

func (e *HfAPIError) Error() string {
	return e.HfAPIErrorResponse.Error
}

func IsHfAPIError(err error) bool {
	_, ok := err.(*HfAPIError)
	return ok
}

//counterfeiter:generate -o fake/hub.go --fake-name FakeHfAPI . HfAPI
type HfAPI interface {
	WhoAmI(ctx context.Context, token string) (*HfAPIWhoAmIResponse, error)
}

type HfAPIClient struct {
	client      *http.Client
	apiEndpoint string
}

// NewHfAPIClient creates a new HfAPIClient.
//
// Source code: https://github.com/huggingface/huggingface_hub/blob/8d1ffc6d78827aa18c4fec3f73843ac7bb64a153/src/huggingface_hub/hf_api.py#L1493-L1535
func NewHfAPIClient() *HfAPIClient {
	return &HfAPIClient{
		client: &http.Client{},
	}
}

func (c *HfAPIClient) endpoint() string {
	if c.apiEndpoint == "" {
		return HubAPIEndpointScheme + HubAPIEndpointDomain
	}

	return c.apiEndpoint
}

// WhoAmI returns the current user.
//
// Source code: https://github.com/huggingface/huggingface_hub/blob/8d1ffc6d78827aa18c4fec3f73843ac7bb64a153/src/huggingface_hub/hf_api.py#L1578-L1607
func (c *HfAPIClient) WhoAmI(ctx context.Context, token string) (*HfAPIWhoAmIResponse, error) {
	req, err := http.NewRequest(http.MethodGet, c.endpoint()+hubAPIEndpointPathWhoAmI, nil)
	if err != nil {
		return nil, err
	}

	req = req.WithContext(ctx)
	req.Header = c.buildHfHeaders(token)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	bodyBuffer := new(bytes.Buffer)
	_, err = bodyBuffer.ReadFrom(resp.Body)
	if err != nil {
		return nil, err
	}

	var errResponse HfAPIErrorResponse
	err = json.Unmarshal(bodyBuffer.Bytes(), &errResponse)
	if err != nil {
		return nil, err
	}
	if errResponse.Error != "" {
		return nil, &HfAPIError{errResponse}
	}

	var whoAmIResponse HfAPIWhoAmIResponse
	err = json.Unmarshal(bodyBuffer.Bytes(), &whoAmIResponse)
	if err != nil {
		return nil, err
	}

	return &whoAmIResponse, nil
}

// Documentations: https://huggingface.co/docs/huggingface_hub/quick-start#authentication
// Source code: https://github.com/huggingface/huggingface_hub/blob/8d1ffc6d78827aa18c4fec3f73843ac7bb64a153/src/huggingface_hub/hf_api.py#L1609-L1629
// References:
// - https://github.com/huggingface/huggingface_hub/blob/8d1ffc6d78827aa18c4fec3f73843ac7bb64a153/src/huggingface_hub/_login.py#L50-L115
// - https://github.com/huggingface/huggingface_hub/blob/8d1ffc6d78827aa18c4fec3f73843ac7bb64a153/src/huggingface_hub/_login.py#L299-L330
func (c *HfAPIClient) GetTokenPermission(ctx context.Context, token string) (string, error) {
	whoAmI, err := c.WhoAmI(ctx, token)
	if err != nil {
		return "", err
	}

	return whoAmI.Auth.AccessToken.Role, nil
}

// Source code: https://github.com/huggingface/huggingface_hub/blob/8d1ffc6d78827aa18c4fec3f73843ac7bb64a153/src/huggingface_hub/utils/_headers.py#L39-L139
// Reference:
// - https://github.com/huggingface/huggingface_hub/blob/8d1ffc6d78827aa18c4fec3f73843ac7bb64a153/src/huggingface_hub/hf_api.py#L9399-L9421
func (c *HfAPIClient) buildHfHeaders(token string) http.Header {
	return http.Header{
		"Authorization": []string{"Bearer " + token},
		"User-Agent":    []string{"hf_hub/4.0.0"},
	}
}
