package sessionprovider

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/aws/aws-xray-sdk-go/xray"
	"github.com/bwmarrin/discordgo"
	"github.com/winebarrel/secretlamb"
)

type Provider func(ctx context.Context) (*discordgo.Session, error)

// ParamStore initialises the Discord Session using the token stored in param store
func ParamStore(paramName string) Provider {
	return func(ctx context.Context) (s *discordgo.Session, err error) {
		ctx, seg := xray.BeginSubsegment(ctx, "param store")
		defer seg.Close(err)
		if paramName == "" {
			return nil, errors.New("empty discord token paramstore parameter name")
		}

		parameters := secretlamb.MustNewParameters()
		parameters.HTTPClient = xray.Client(parameters.HTTPClient)

		p, err := parameters.GetWithContext(ctx, paramName, secretlamb.ParameterWithDecryption())
		if err != nil {
			return nil, err
		}

		if p == nil || p.Parameter.Value == "" {
			return nil, fmt.Errorf("parameter empty")
		}

		s, _ = discordgo.New("Bot " + p.Parameter.Value)
		s.Client = xray.Client(s.Client)

		return s, nil
	}
}

// Cached wraps a Provider, ensuring it is only called until it succeeds. Errors are not cached, so a transient failure
// (e.g. fetching the token) is retried on the next call rather than failing every call for the lifetime of the process.
func Cached(f Provider) Provider {
	var v *discordgo.Session
	var mu sync.Mutex

	return func(ctx context.Context) (*discordgo.Session, error) {
		mu.Lock()
		defer mu.Unlock()

		if v != nil {
			return v, nil
		}

		s, err := f(ctx)
		if err != nil {
			return nil, err
		}

		v = s

		return v, nil
	}
}

// Static will always return the provided session.
func Static(s *discordgo.Session) Provider {
	return func(ctx context.Context) (*discordgo.Session, error) {
		return s, nil
	}
}
