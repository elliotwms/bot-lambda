package sessionprovider

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSessionFromParamStore(t *testing.T) {
	given, when, then := NewSessionStage(t)

	given.
		a_parameter_named_x_with_value_y("foo", "bar")

	when.
		a_new_session_from_param_store_is_requested_with_param_named("foo")

	then.
		no_error_should_be_returned().and().
		the_session_has_token("Bot bar")
}

func TestSessionFromParamStore_EmptyParamName(t *testing.T) {
	given, when, then := NewSessionStage(t)

	given.
		a_parameter_named_x_with_value_y("foo", "bar")

	when.
		a_new_session_from_param_store_is_requested_with_param_named("")

	then.
		an_error_should_be_returned("empty discord token paramstore parameter name")
}

func TestSessionFromParamStore_HttpError(t *testing.T) {
	given, when, then := NewSessionStage(t)

	given.
		the_param_store_server_is_unavailable()

	when.
		a_new_session_from_param_store_is_requested_with_param_named("foo")

	then.
		an_error_should_be_returned("failed to get parameter - http request error")
}

func TestSessionFromParamStore_EmptyParamValue(t *testing.T) {
	given, when, then := NewSessionStage(t)

	given.
		a_parameter_named_x_with_value_y("foo", "")

	when.
		a_new_session_from_param_store_is_requested_with_param_named("foo")

	then.
		an_error_should_be_returned("parameter empty")
}

func TestCached(t *testing.T) {
	count := 0
	f := func(ctx context.Context) (*discordgo.Session, error) {
		count++

		return &discordgo.Session{
			Token: fmt.Sprintf("Bot %v", count), // ensure the value changes with subsequent calls
		}, nil
	}

	source := Cached(f)

	v1, _ := source(context.Background())
	v2, _ := source(context.Background())

	require.Equal(t, 1, count)
	require.Equal(t, v1, v2)
}
