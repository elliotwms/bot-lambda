package bot_lambda

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/bwmarrin/discordgo"
	"github.com/elliotwms/bot/interactions/router"
)

// WithCommand registers an application command with the underlying Router. The definition is also kept so that the
// command can be registered with Discord, see RegisterCommands and TaskRegisterCommands.
func (e *Endpoint) WithCommand(cmd *discordgo.ApplicationCommand, handler router.ApplicationCommandHandler) *Endpoint {
	e.router.RegisterCommand(cmd.Name, cmd.Type, handler)
	e.commands = append(e.commands, cmd)

	return e
}

// Commands returns the definitions of the commands registered with the endpoint.
func (e *Endpoint) Commands() []*discordgo.ApplicationCommand {
	return slices.Clone(e.commands)
}

// Application is the bot's application, as returned by Discord's Get Current Application endpoint.
// See https://discord.com/developers/docs/resources/application#get-current-application.
type Application struct {
	ID                          string `json:"id"`
	Name                        string `json:"name"`
	ApproximateGuildCount       int    `json:"approximate_guild_count"`
	ApproximateUserInstallCount int    `json:"approximate_user_install_count"`
}

// CurrentApplication returns the application of the session's bot token.
func CurrentApplication(ctx context.Context, s *discordgo.Session) (*Application, error) {
	body, err := s.RequestWithBucketID(
		"GET",
		discordgo.EndpointApplication("@me"),
		nil,
		discordgo.EndpointApplication(""),
		discordgo.WithContext(ctx),
	)
	if err != nil {
		return nil, fmt.Errorf("get current application: %w", err)
	}

	var app *Application
	if err := json.Unmarshal(body, &app); err != nil {
		return nil, fmt.Errorf("unmarshal current application: %w", err)
	}

	return app, nil
}

// RegisterCommands overwrites the global commands of the session's application with commands. Commands which already
// exist (by name and type) keep their IDs, and so any permissions configured by servers. Any other existing commands
// are deleted.
func RegisterCommands(ctx context.Context, s *discordgo.Session, commands []*discordgo.ApplicationCommand) error {
	app, err := CurrentApplication(ctx, s)
	if err != nil {
		return err
	}

	if commands == nil {
		// an empty list deletes all commands, whereas null is rejected
		commands = []*discordgo.ApplicationCommand{}
	}

	if _, err := s.ApplicationCommandBulkOverwrite(app.ID, "", commands, discordgo.WithContext(ctx)); err != nil {
		return fmt.Errorf("overwrite commands: %w", err)
	}

	return nil
}

func registerCommandsTask(ctx context.Context, e *Endpoint, s *discordgo.Session) error {
	return RegisterCommands(ctx, s, e.Commands())
}
