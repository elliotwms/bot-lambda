package sessionprovider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/aws/aws-xray-sdk-go/xray"
	"github.com/bwmarrin/discordgo"
	"github.com/stretchr/testify/require"
	"github.com/winebarrel/secretlamb"
)

type SessionStage struct {
	t       *testing.T
	require *require.Assertions
	session *discordgo.Session
	err     error
}

func NewSessionStage(t *testing.T) (*SessionStage, *SessionStage, *SessionStage) {
	s := &SessionStage{
		t:       t,
		require: require.New(t),
	}

	return s, s, s
}

func (s *SessionStage) and() *SessionStage {
	return s
}

func (s *SessionStage) a_parameter_named_x_with_value_y(x, y string) *SessionStage {
	return s.param_store_will_return(func(w http.ResponseWriter, r *http.Request) {
		bs, _ := json.Marshal(secretlamb.ParameterOutput{
			Parameter: secretlamb.ParameterOutputParameter{
				Name:  x,
				Value: y,
			},
		})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(bs)
	})
}

func (s *SessionStage) param_store_will_return(f http.HandlerFunc) *SessionStage {
	server := httptest.NewServer(f)
	s.t.Cleanup(server.Close)

	u, err := url.Parse(server.URL)
	s.require.NoError(err)

	s.t.Setenv("PARAMETERS_SECRETS_EXTENSION_HTTP_PORT", u.Port())

	return s
}

func (s *SessionStage) a_new_session_from_param_store_is_requested_with_param_named(name string) *SessionStage {
	ctx, _ := xray.BeginSegment(context.Background(), "test")

	s.session, s.err = ParamStore(name)(ctx)

	return s
}

func (s *SessionStage) no_error_should_be_returned() *SessionStage {
	s.require.NoError(s.err)

	return s

}

func (s *SessionStage) the_session_has_token(token string) {
	s.require.NotNil(s.session)
	s.require.Equal(token, s.session.Token)
}

func (s *SessionStage) an_error_should_be_returned(err string) {
	s.require.ErrorContains(s.err, err)
}

func (s *SessionStage) the_param_store_server_is_unavailable() *SessionStage {
	return s.param_store_will_return(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
}
