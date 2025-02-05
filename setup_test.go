package lambda

import (
	"os"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/elliotwms/fakediscord/pkg/fakediscord"
)

const testToken = "bot"

var (
	session *discordgo.Session
)

func TestMain(m *testing.M) {
	fakediscord.Configure("http://localhost:8080/")

	openSession()

	code := m.Run()

	closeSession()

	os.Exit(code)
}

func openSession() {
	var err error
	session, err = discordgo.New("Bot " + testToken)
	if err != nil {
		panic(err)
	}

	if os.Getenv("TEST_DEBUG") != "" {
		session.LogLevel = discordgo.LogDebug
		session.Debug = true
	}

	session.Identify.Intents = discordgo.IntentsGuilds |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsGuildMessageReactions

	// session is used for asserting on events from fakediscord
	if err := session.Open(); err != nil {
		panic(err)
	}
}

func closeSession() {
	if err := session.Close(); err != nil {
		panic(err)
	}
}
