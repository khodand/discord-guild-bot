package main

import (
	"context"
	"github.com/diamondburned/arikawa/v3/discord"
	"log"
	"sync"

	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/state"
)

const token = "***"

func main() {
	s := state.New("Bot " + token)
	bot := bot{
		mx:    &sync.RWMutex{},
		state: s,
		users: make(map[discord.UserID]discord.RoleID, 100),
		roles: make(map[discord.RoleID]PollRole, 10),
	}

	app, err := s.CurrentApplication()
	if err != nil {
		log.Fatalln("failed to get application ID:", err)
	}
	s.AddHandler(bot.HandleInteractionCreateEvent)

	s.AddIntents(gateway.IntentGuilds)
	s.AddIntents(gateway.IntentGuildMessages)

	if err := s.Open(context.Background()); err != nil {
		log.Fatalln("failed to open:", err)
	}
	defer s.Close()

	if _, err := s.BulkOverwriteCommands(app.ID, commands); err != nil {
		log.Fatalln("failed to create guild command:", err)
	}

	log.Println("Commands created. Bot is ready.")

	select {}
}
