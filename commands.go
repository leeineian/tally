package main

import (
	"database/sql"
	"fmt"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
)

var commands = []discord.ApplicationCommandCreate{
	discord.SlashCommandCreate{
		Name:        "about",
		Description: "Information about the bot",
	},
	discord.SlashCommandCreate{
		Name:        "current",
		Description: "Get the current count",
	},
	discord.SlashCommandCreate{
		Name:        "help",
		Description: "Get help",
	},
	discord.SlashCommandCreate{
		Name:        "invite",
		Description: "Invite the bot to your server",
	},
	discord.SlashCommandCreate{
		Name:        "ping",
		Description: "Check bot latency",
	},
	discord.SlashCommandCreate{
		Name:        "rules",
		Description: "View rules",
	},
	discord.SlashCommandCreate{
		Name:                     "set-channel",
		Description:              "Set the counting channel",
		DefaultMemberPermissions: discord.PermissionManageChannels,
		Options: []discord.ApplicationCommandOption{
			discord.ApplicationCommandOptionChannel{
				Name:        "channel",
				Description: "The channel to count in",
				Required:    true,
				ChannelTypes: []discord.ChannelType{
					discord.ChannelTypeGuildText,
				},
			},
		},
	},
	discord.SlashCommandCreate{
		Name:                     "set",
		Description:              "Set the current count",
		DefaultMemberPermissions: discord.PermissionManageMessages,
		Options: []discord.ApplicationCommandOption{
			discord.ApplicationCommandOptionInt{
				Name:        "count",
				Description: "The count to set",
				Required:    true,
			},
		},
	},
}

func registerCommands(client bot.Client) {
	_, err := client.Rest().SetGlobalCommands(client.ApplicationID(), commands)
	if err != nil {
		fmt.Println("error registering commands: ", err)
	}
}

func commandHandler(db *sql.DB) func(event *events.ApplicationCommandInteractionCreate) {
	return func(event *events.ApplicationCommandInteractionCreate) {
		data := event.SlashCommandInteractionData()
		switch data.CommandName() {
		case "about":
			event.CreateMessage(discord.MessageCreate{
				Content: "Tally counting bot rewritten in Go.",
			})
		case "current":
			if event.GuildID() == nil {
				return
			}
			entry, _ := getCountEntry(db, event.GuildID().String())
			event.CreateMessage(discord.MessageCreate{
				Content: fmt.Sprintf("The current count is %d", entry.Count),
			})
		case "help":
			event.CreateMessage(discord.MessageCreate{
				Content: "Start counting by typing numbers in the designated channel.",
			})
		case "invite":
			event.CreateMessage(discord.MessageCreate{
				Content: "https://discord.com/api/oauth2/authorize?client_id=" + event.Client().ApplicationID().String() + "&permissions=8&scope=bot%20applications.commands",
			})
		case "ping":
			event.CreateMessage(discord.MessageCreate{
				Content: "Pong!",
			})
		case "rules":
			event.CreateMessage(discord.MessageCreate{
				Content: "No rules configured yet.",
			})
		case "set-channel":
			if event.GuildID() == nil {
				return
			}
			channelID := data.Snowflake("channel")
			config, _ := getGuildConfig(db, event.GuildID().String())
			
			webhook, err := event.Client().Rest().CreateWebhook(channelID, discord.WebhookCreate{
				Name: "Tally Webhook",
			})
			if err != nil {
				event.CreateMessage(discord.MessageCreate{
					Content: "Failed to create webhook in channel.",
				})
				return
			}

			config.Active = true
			config.Channel = channelID.String()
			config.WebhookID = webhook.ID().String()
			config.WebhookToken = webhook.Token
			setGuildConfig(db, config)
			event.CreateMessage(discord.MessageCreate{
				Content: fmt.Sprintf("Counting channel set to <#%s>", channelID.String()),
			})
		case "set":
			if event.GuildID() == nil {
				return
			}
			count := data.Int("count")
			entry, _ := getCountEntry(db, event.GuildID().String())
			entry.Count = count
			entry.LastCounter = ""
			setCountEntry(db, entry)
			event.CreateMessage(discord.MessageCreate{
				Content: fmt.Sprintf("Count set to %d", count),
			})
		}
	}
}
