package modelscope

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogin(t *testing.T) {
	t.Run("Default", func(t *testing.T) {
		c := NewHubAPIClient()

		var serverHandled bool
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			serverHandled = true

			if req.URL.Path != hubAPIEndpointPathLogin {
				t.Error("invalid path")
				return
			}

			rw.WriteHeader(http.StatusOK)
			_, err := rw.Write(lo.Must(json.Marshal(&HubAPIBaseResponse[HubAPILoginResponse]{
				Code: 200,
				Data: &HubAPILoginResponse{
					AccessToken: "token",
					Email:       "email",
					Username:    "username",
					WorkNo:      "work",
				},
				Message:   "success",
				RequestID: uuid.New().String(),
				Success:   true,
			})))
			require.NoError(t, err)
		}))
		defer server.Close()

		c.apiEndpoint = server.URL
		c.client = server.Client()

		loginResp, err := c.Login(context.Background(), "token")
		require.NoError(t, err)
		require.NotNil(t, loginResp)
		require.True(t, serverHandled)

		assert.Equal(t, int64(200), loginResp.Code)
		assert.Equal(t, "token", loginResp.Data.AccessToken)
		assert.Equal(t, "email", loginResp.Data.Email)
		assert.Equal(t, "username", loginResp.Data.Username)
		assert.Equal(t, "work", loginResp.Data.WorkNo)
	})

	t.Run("Error - invalid token", func(t *testing.T) {
		c := NewHubAPIClient()

		var serverHandled bool
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			serverHandled = true

			if req.URL.Path != hubAPIEndpointPathLogin {
				t.Error("invalid path")
				return
			}

			rw.WriteHeader(http.StatusOK)
			_, err := rw.Write(lo.Must(json.Marshal(&HubAPIBaseResponse[HubAPILoginResponse]{
				Code:      10010103009,
				Message:   "登录失败，AccessToken错误，请从用户中心获取AccessToken或刷新",
				RequestID: uuid.New().String(),
				Success:   false,
			})))
			require.NoError(t, err)
		}))
		defer server.Close()

		c.apiEndpoint = server.URL
		c.client = server.Client()

		loginResp, err := c.Login(context.Background(), "token")
		require.Error(t, err)
		require.Nil(t, loginResp)
		require.True(t, serverHandled)
		assert.EqualError(t, err, "登录失败，AccessToken错误，请从用户中心获取AccessToken或刷新")

		errResp, ok := err.(*HubAPIError)
		require.True(t, ok)
		require.NotNil(t, errResp)

		assert.Equal(t, int64(10010103009), errResp.Code)
		assert.Empty(t, errResp.Data)
		assert.Equal(t, "登录失败，AccessToken错误，请从用户中心获取AccessToken或刷新", errResp.Message)
		assert.False(t, errResp.Success)
	})
}
