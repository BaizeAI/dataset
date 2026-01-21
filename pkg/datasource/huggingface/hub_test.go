package huggingface

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWhoAmI(t *testing.T) {
	t.Run("Default", func(t *testing.T) {
		c := NewHfAPIClient()

		var serverHandled bool
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			serverHandled = true

			if req.URL.Path != hubAPIEndpointPathWhoAmI {
				t.Error("invalid path")
				return
			}

			rw.WriteHeader(http.StatusOK)
			_, err := rw.Write(lo.Must(json.Marshal(&HfAPIWhoAmIResponse{
				Auth: HfAPIWhoAmIResponseAuth{
					AccessToken: HfAPIAccessToken{
						CreatedAt:   time.Now(),
						DisplayName: "token-name",
						Role:        "read",
					},
				},
				AvatarURL:     "https://example.com/avatar-url.png",
				CanPay:        false,
				Email:         "user@example.com",
				EmailVerified: true,
				Fullname:      "User Name",
				ID:            "643ba8e3b409fef15e05aa37",
				IsPro:         false,
				Name:          "username",
				Type:          "user",
			})))
			require.NoError(t, err)
		}))
		defer server.Close()

		c.apiEndpoint = server.URL
		c.client = server.Client()

		whoAmIResp, err := c.WhoAmI(context.Background(), "token")
		require.NoError(t, err)
		require.NotNil(t, whoAmIResp)
		require.True(t, serverHandled)

		assert.False(t, whoAmIResp.Auth.AccessToken.CreatedAt.IsZero())
		assert.Equal(t, "token-name", whoAmIResp.Auth.AccessToken.DisplayName)
		assert.Equal(t, "read", whoAmIResp.Auth.AccessToken.Role)
		assert.Equal(t, "https://example.com/avatar-url.png", whoAmIResp.AvatarURL)
		assert.False(t, whoAmIResp.CanPay)
		assert.Equal(t, "user@example.com", whoAmIResp.Email)
		assert.True(t, whoAmIResp.EmailVerified)
		assert.Equal(t, "User Name", whoAmIResp.Fullname)
		assert.Equal(t, "643ba8e3b409fef15e05aa37", whoAmIResp.ID)
		assert.False(t, whoAmIResp.IsPro)
		assert.Equal(t, "username", whoAmIResp.Name)
		assert.Equal(t, "user", whoAmIResp.Type)
	})

	t.Run("Error - invalid token", func(t *testing.T) {
		c := NewHfAPIClient()

		var serverHandled bool
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			serverHandled = true

			if req.URL.Path != hubAPIEndpointPathWhoAmI {
				t.Error("invalid path")
				return
			}

			rw.WriteHeader(http.StatusOK)
			_, err := rw.Write(lo.Must(json.Marshal(&HfAPIErrorResponse{
				Error: "Invalid username or password.",
			})))
			require.NoError(t, err)
		}))
		defer server.Close()

		c.apiEndpoint = server.URL
		c.client = server.Client()

		whoAmIResp, err := c.WhoAmI(context.Background(), "token")
		require.Error(t, err)
		require.Nil(t, whoAmIResp)
		require.True(t, serverHandled)
		assert.EqualError(t, err, "Invalid username or password.")

		errResp, ok := err.(*HfAPIError)
		require.True(t, ok)
		require.NotNil(t, errResp)

		assert.Equal(t, "Invalid username or password.", errResp.HfAPIErrorResponse.Error)
	})
}
