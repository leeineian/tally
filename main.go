package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
	"github.com/joho/godotenv"
)

var (
	cleanup = flag.Bool("cleanup", false, "Clean up all slash commands and exit")
	refresh = flag.Bool("refresh", false, "Refresh all slash commands")
	guild   = flag.String("guild", "", "Specific Guild ID to apply cleanup/refresh to (optional)")
)

func createCountingWebhook(client *bot.Client, channelID snowflake.ID) (discord.Webhook, error) {
	webhookName := os.Getenv("WEBHOOK_NAME")
	if webhookName == "" {
		webhookName = "webhook"
	}

	var avatarIcon *discord.Icon
	if selfUser, err := client.Rest.GetUser(client.ApplicationID); err == nil && selfUser.AvatarURL() != nil {
		resp, err := http.Get(*selfUser.AvatarURL())
		if err == nil {
			defer resp.Body.Close()
			
			iconType := discord.IconTypePNG
			contentType := resp.Header.Get("Content-Type")
			if strings.Contains(contentType, "jpeg") || strings.Contains(contentType, "jpg") {
				iconType = discord.IconTypeJPEG
			} else if strings.Contains(contentType, "gif") {
				iconType = discord.IconTypeGIF
			}

			icon, err := discord.NewIcon(iconType, resp.Body)
			if err == nil {
				avatarIcon = icon
			}
		}
	}

	return client.Rest.CreateWebhook(channelID, discord.WebhookCreate{
		Name:   webhookName,
		Avatar: avatarIcon,
	})
}

func main() {
	flag.Parse()
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
			gateway.WithIntents(
				gateway.IntentGuilds,
				gateway.IntentGuildMessages,
				gateway.IntentMessageContent,
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

	if *cleanup {
		if *guild != "" {
			gID, _ := snowflake.Parse(*guild)
			_, err = client.Rest.SetGuildCommands(client.ApplicationID, gID, []discord.ApplicationCommandCreate{})
			if err != nil {
				fmt.Println("error cleaning up guild commands:", err)
			} else {
				fmt.Println("Cleaned up guild commands for", *guild)
			}
		} else {
			_, err = client.Rest.SetGlobalCommands(client.ApplicationID, []discord.ApplicationCommandCreate{})
			if err != nil {
				fmt.Println("error cleaning up global commands:", err)
			} else {
				fmt.Println("Cleaned up global commands.")
			}
		}
		return
	}

	if *refresh {
		if *guild != "" {
			gID, _ := snowflake.Parse(*guild)
			_, err = client.Rest.SetGuildCommands(client.ApplicationID, gID, commands)
			if err != nil {
				fmt.Println("error refreshing guild commands:", err)
			} else {
				fmt.Println("Refreshed guild commands for", *guild)
			}
		} else {
			registerCommands(client)
			fmt.Println("Refreshed global commands.")
		}
	}

	if selfUser, err := client.Rest.GetUser(client.ApplicationID); err == nil {
		fmt.Printf("%s (%s) is up. Press CTRL-C to exit.\n", selfUser.Username, client.ApplicationID.String())
	} else {
		fmt.Printf("Bot (%s) is up. Press CTRL-C to exit.\n", client.ApplicationID.String())
	}
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
			_ = event.Client().Rest.DeleteMessage(event.ChannelID, event.MessageID)
			return
		}

		if entry.LastCounter == event.Message.Author.ID.String() && !config.AllowDoublePost {
			_ = event.Client().Rest.DeleteMessage(event.ChannelID, event.MessageID)
			ch, err := event.Client().Rest.CreateDMChannel(event.Message.Author.ID)
			if err == nil {
				_, _ = event.Client().Rest.CreateMessage(ch.ID(), discord.MessageCreate{
					Content: fmt.Sprintf("You have already counted in <#%s>! Wait for someone else to count before you count again.", event.ChannelID.String()),
				})
			}
			return
		}

		_ = event.Client().Rest.DeleteMessage(event.ChannelID, event.MessageID)

		entry.Count = nextCount
		entry.LastCounter = event.Message.Author.ID.String()
		_ = setCountEntry(db, entry)



		webhookID, _ := snowflake.Parse(config.WebhookID)
		
		avatarURL := ""
		if url := event.Message.Author.AvatarURL(); url != nil {
			avatarURL = *url
		}

		apiMsg, err := event.Client().Rest.CreateWebhookMessage(webhookID, config.WebhookToken, discord.WebhookMessageCreate{
			Content:   strconv.Itoa(nextCount),
			Username:  event.Message.Author.Username,
			AvatarURL: avatarURL,
		}, rest.CreateWebhookMessageParams{Wait: true})
		
		if err != nil {
			newWebhook, hwErr := createCountingWebhook(event.Client(), event.ChannelID)
			if hwErr == nil {
				config.WebhookID = newWebhook.ID().String()
				config.WebhookToken = newWebhook.(*discord.IncomingWebhook).Token
				setGuildConfig(db, config)
				
				webhookID = newWebhook.ID()
				apiMsg, err = event.Client().Rest.CreateWebhookMessage(webhookID, config.WebhookToken, discord.WebhookMessageCreate{
					Content:   strconv.Itoa(nextCount),
					Username:  event.Message.Author.Username,
					AvatarURL: avatarURL,
				}, rest.CreateWebhookMessageParams{Wait: true})
			}
		}

		var apiMsgID snowflake.ID
		if err == nil && apiMsg != nil {
			apiMsgID = apiMsg.ID
			_ = saveCountMessage(db, event.GuildID.String(), nextCount, apiMsgID.String())
		}

		rules, _ := getGuildRules(db, event.GuildID.String())
		for _, rule := range rules {
			match := false
			if rule.Type == "equals" && nextCount == rule.Value {
				match = true
			} else if rule.Type == "multiple_of" && rule.Value != 0 && nextCount%rule.Value == 0 {
				match = true
			}
			if !match {
				continue
			}

			ruleContent := strings.ReplaceAll(rule.ActionV1, "{{count}}", strconv.Itoa(nextCount))

			switch rule.Action {
			case "pin":
				if apiMsgID != 0 {
					_ = event.Client().Rest.PinMessage(event.ChannelID, apiMsgID)
				}
			case "dm":
				ch, err := event.Client().Rest.CreateDMChannel(event.Message.Author.ID)
				if err == nil {
					_, _ = event.Client().Rest.CreateMessage(ch.ID(), discord.MessageCreate{
						Content: ruleContent,
					})
				}
			case "msg":
				_, _ = event.Client().Rest.CreateMessage(event.ChannelID, discord.MessageCreate{
					Content: ruleContent,
				})
			}
		}
	}
}
