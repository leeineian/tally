package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()
	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		fmt.Println("BOT_TOKEN is not set")
		return
	}

	db := initDB()
	defer db.Close()

	client, err := disgo.New(token,
		bot.WithGatewayConfigOpts(
			discord.WithIntents(
				discord.IntentGuilds,
				discord.IntentGuildMessages,
				discord.IntentMessageContent,
			),
		),
		bot.WithEventListeners(
			&events.ListenerAdapter{
				OnApplicationCommandInteraction: commandHandler(db),
				OnMessageCreate:                 messageHandler(db),
			},
		),
	)
	if err != nil {
		fmt.Println("error building client: ", err)
		return
	}

	if err = client.OpenGateway(context.TODO()); err != nil {
		fmt.Println("error connecting to gateway: ", err)
		return
	}

	registerCommands(client)
	fmt.Println("Bot is running. Press CTRL-C to exit.")
	s := make(chan os.Signal, 1)
	signal.Notify(s, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-s
	client.Close(context.TODO())
}

func messageHandler(db *sql.DB) func(event *events.MessageCreate) {
	return func(event *events.MessageCreate) {
		if event.Message.Author.Bot || event.GuildID == nil {
			return
		}

		config, _ := getGuildConfig(db, event.GuildID.String())
		if !config.Active || config.Channel != event.Message.ChannelID.String() {
			return
		}

		content := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, event.Message.Content)

		providedInt, err := strconv.Atoi(content)
		if err != nil {
			return
		}

		entry, _ := getCountEntry(db, event.GuildID.String())
		nextCount := entry.Count + 1

		if providedInt != nextCount {
			_ = event.Client().Rest().DeleteMessage(event.ChannelID, event.MessageID)
			return
		}

		if entry.LastCounter == event.Message.Author.ID.String() && !config.AllowDoublePost {
			_ = event.Client().Rest().DeleteMessage(event.ChannelID, event.MessageID)
			ch, err := event.Client().Rest().CreateDMChannel(event.Message.Author.ID)
			if err == nil {
				_, _ = event.Client().Rest().CreateMessage(ch.ID(), discord.MessageCreate{
					Content: fmt.Sprintf("You have already counted in <#%s>! Wait for someone else to count before you count again.", event.ChannelID.String()),
				})
			}
			return
		}

		_ = event.Client().Rest().DeleteMessage(event.ChannelID, event.MessageID)

		entry.Count = nextCount
		entry.LastCounter = event.Message.Author.ID.String()
		_ = setCountEntry(db, entry)

		webhookID, _ := discord.ParseSnowflake(config.WebhookID)
		_, _ = event.Client().Rest().CreateWebhookMessage(webhookID, config.WebhookToken, discord.WebhookMessageCreate{
			Content:   strconv.Itoa(nextCount),
			Username:  event.Message.Author.Username,
			AvatarURL: event.Message.Author.AvatarURL(),
		}, true, 0)
	}
}
