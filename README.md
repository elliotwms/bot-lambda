# bot-lambda

A lightweight framework which provides an endpoint for Discord bots to respond to [Discord Interactions](https://discord.com/developers/docs/interactions/overview) using AWS Lambda functions.

## Usage

> [!TIP]
> Prefer a real example? Check out [elliotwms/pinbot-lambda](https://github.com/elliotwms/pinbot-lambda)

```go
package main

import (
	"context"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/bwmarrin/discordgo"
	"github.com/elliotwms/bot-lambda"
)

func main() {
	bot := bot_lambda.
		New([]byte("publicKey")).
		WithMessageApplicationCommand("Foo", handleFoo)

	lambda.Start(bot.HandleRequest)
}

func handleFoo(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) (err error) {
	// your handler code
	return nil
}

```

## Features

### Lambda Function URL Support

Lambda functions receive different kinds of events depending on how they are invoked. bot-lambda provides a handler for both API Gateway and Function URL invocation types.

For API Gateway use `HandleEvent`, and for Function URLs use `HandleRequest`.

### Configurable Interaction Router

The underlying interaction router can be configured to provide initial deferred responses, which can be useful when handlers exceed the 3-second initial response time limit (this can often be the case during cold starts or when your downstream dependencies ).

### Built-in Ping Request Handling

bot-lambda responds to PING requests from Discord as described in the [Discord documentation](https://discord.com/developers/docs/interactions/overview#setting-up-an-endpoint-acknowledging-ping-requests).

### Public Key Verification

bot-lambda validates security headers sent by Discord as described in the [documentation](https://discord.com/developers/docs/interactions/overview#setting-up-an-endpoint-validating-security-request-headers) using the provided public key.

### Session Providers

Bots will often need to use a more broadly scoped token than that provided in the interaction request for callbacks. When configured, bot-lambda replaces the `discordgo.Session` received by the command handler with the one resolved by the session provider.

There are a couple of built-in session providers, including retrieving the token from AWS SYstems Manager Parameter Store as used in the reference implementation. See [the `sessionprovider` package](/sessionprovider) for more info.

### X-Ray Tracing

The endpoint is fully traced using Amazon X-Ray, including the Discord clients provided to the handlers. Use the context provided to continue tracing within your handlers using the X-Ray SDK.

### Logging

Provide a slog logger to receive debug logs from both the Endpoint and the Router.