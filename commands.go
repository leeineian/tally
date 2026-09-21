package main

import (
	"database/sql"
	"fmt"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
)

var commands = []discord.ApplicationCommandCreate{
	discord.SlashCommandCreate{
		Name:        "about",
		Description: "Information about the bot",
	},
	discord.SlashCommandCreate{
		Name:        "current-count",
		Description: "Get the current count",
	},
	discord.SlashCommandCreate{
		Name:        "rule",
		Description: "Manage counting rules",
		Options: []discord.ApplicationCommandOption{
			discord.ApplicationCommandOptionSubCommand{
				Name:        "create",
				Description: "Create a new rule",
				Options: []discord.ApplicationCommandOption{
					discord.ApplicationCommandOptionString{
						Name:        "action",
						Description: "Action to perform",
						Required:    true,
						Choices: []discord.ApplicationCommandOptionChoiceString{
							{Name: "Pin Message", Value: "pin"},
							{Name: "Send DM", Value: "dm"},
							{Name: "Send Message", Value: "msg"},
						},
					},
					discord.ApplicationCommandOptionString{
						Name:        "type",
						Description: "Condition type",
						Required:    true,
						Choices: []discord.ApplicationCommandOptionChoiceString{
							{Name: "Equals", Value: "equals"},
							{Name: "Multiple Of", Value: "multiple_of"},
						},
					},
					discord.ApplicationCommandOptionInt{
						Name:        "value",
						Description: "Value for condition",
						Required:    true,
					},
					discord.ApplicationCommandOptionString{
						Name:        "content",
						Description: "Content to send (use {{count}} for count) (ignored for pin)",
						Required:    false,
					},
				},
			},
			discord.ApplicationCommandOptionSubCommand{
				Name:        "delete",
				Description: "Delete a specific rule",
				Options: []discord.ApplicationCommandOption{
					discord.ApplicationCommandOptionString{
						Name:        "id",
						Description: "Rule ID",
						Required:    true,
					},
				},
			},
			discord.ApplicationCommandOptionSubCommand{
				Name:        "view",
				Description: "View a specific rule",
				Options: []discord.ApplicationCommandOption{
					discord.ApplicationCommandOptionString{
						Name:        "id",
						Description: "Rule ID",
						Required:    true,
					},
				},
			},
			discord.ApplicationCommandOptionSubCommand{
				Name:        "list",
				Description: "List all rules",
			},
		},
	},
	discord.SlashCommandCreate{
		Name:        "settings",
		Description: "Configure tally settings",
		Options: []discord.ApplicationCommandOption{
			discord.ApplicationCommandOptionSubCommand{
				Name:        "current-count",
				Description: "Set the current count",
				Options: []discord.ApplicationCommandOption{
					discord.ApplicationCommandOptionInt{
						Name:        "number",
						Description: "The count to set",
						Required:    true,
					},
					discord.ApplicationCommandOptionString{
						Name:        "purge",
						Description: "Purge messages down to this count?",
						Required:    false,
						Choices: []discord.ApplicationCommandOptionChoiceString{
							{Name: "Yes", Value: "yes"},
							{Name: "No", Value: "no"},
						},
					},
				},
			},
			discord.ApplicationCommandOptionSubCommand{
				Name:        "channel",
				Description: "Set the counting channel",
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
			discord.ApplicationCommandOptionSubCommand{
				Name:        "take-turns",
				Description: "Toggle consecutive counting",
				Options: []discord.ApplicationCommandOption{
					discord.ApplicationCommandOptionString{
						Name:        "state",
						Description: "State (on forces taking turns, off allows consecutive counting)",
						Required:    true,
						Choices: []discord.ApplicationCommandOptionChoiceString{
							{Name: "On", Value: "on"},
							{Name: "Off", Value: "off"},
						},
					},
				},
			},
			discord.ApplicationCommandOptionSubCommand{
				Name:        "view",
				Description: "View current settings",
			},
		},
	},
}

func registerCommands(client *bot.Client) {
	_, err := client.Rest.SetGlobalCommands(client.ApplicationID, commands)
	if err != nil {
		fmt.Println("error registering commands: ", err)
	}
}

func commandHandler(db *sql.DB) func(event *events.ApplicationCommandInteractionCreate) {
	return func(event *events.ApplicationCommandInteractionCreate) {
		data := event.SlashCommandInteractionData()
		switch data.CommandName() {
		case "about":
			content := fmt.Sprintf("Tally counting bot rewritten in Go.\n\n**Help:** Start counting by typing numbers in the designated channel.\n**Invite:** https://discord.com/api/oauth2/authorize?client_id=%s&permissions=8&scope=bot%%20applications.commands\n**Ping:** %dms", event.Client().ApplicationID.String(), event.Client().Gateway.Latency().Milliseconds())
			event.CreateMessage(discord.MessageCreate{
				Content: content,
				Flags:   discord.MessageFlagEphemeral,
			})
		case "current-count":
			if event.GuildID() == nil {
				return
			}
			entry, _ := getCountEntry(db, event.GuildID().String())
			event.CreateMessage(discord.MessageCreate{
				Content: fmt.Sprintf("The current count is %d", entry.Count),
				Flags:   discord.MessageFlagEphemeral,
			})
		case "rule":
			if event.GuildID() == nil {
				return
			}
			sub := *data.SubCommandName
			switch sub {
			case "create":
				action := data.String("action")
				typ := data.String("type")
				val := data.Int("value")
				content := data.String("content")
				if content == "" && action != "pin" {
					content = "Triggered rule at count {{count}}!"
				}
				id := fmt.Sprintf("%x", event.ID())
				r := GuildRule{
					ID:       id,
					Guild:    event.GuildID().String(),
					Trigger:  "count",
					Type:     typ,
					Value:    val,
					Action:   action,
					ActionV1: content,
				}
				_ = addGuildRule(db, r)
				event.CreateMessage(discord.MessageCreate{
					Content: fmt.Sprintf("Created rule `%s`:\nAction: %s\nCondition: %s %d", id, action, typ, val),
					Flags:   discord.MessageFlagEphemeral,
				})
			case "delete":
				id := data.String("id")
				_ = deleteGuildRule(db, id, event.GuildID().String())
				event.CreateMessage(discord.MessageCreate{
					Content: fmt.Sprintf("Deleted rule `%s`", id),
					Flags:   discord.MessageFlagEphemeral,
				})
			case "view":
				id := data.String("id")
				r, err := getGuildRule(db, id, event.GuildID().String())
				if err != nil {
					event.CreateMessage(discord.MessageCreate{
						Content: "Rule not found.",
						Flags:   discord.MessageFlagEphemeral,
					})
					return
				}
				event.CreateMessage(discord.MessageCreate{
					Content: fmt.Sprintf("Rule `%s`:\nAction: %s\nType: %s\nValue: %d\nContent: %s", r.ID, r.Action, r.Type, r.Value, r.ActionV1),
					Flags:   discord.MessageFlagEphemeral,
				})
			case "list":
				rules, _ := getGuildRules(db, event.GuildID().String())
				if len(rules) == 0 {
					event.CreateMessage(discord.MessageCreate{
						Content: "No rules configured.",
						Flags:   discord.MessageFlagEphemeral,
					})
					return
				}
				out := "Rules:\n"
				for _, r := range rules {
					out += fmt.Sprintf("- `%s`: %s on %s %d\n", r.ID, r.Action, r.Type, r.Value)
				}
				event.CreateMessage(discord.MessageCreate{
					Content: out,
					Flags:   discord.MessageFlagEphemeral,
				})
			}
		case "settings":
			if event.GuildID() == nil {
				return
			}
			sub := *data.SubCommandName
			switch sub {
			case "current-count":
				_ = event.DeferCreateMessage(true)
				
				num := data.Int("number")

				purgeOpt, ok := data.OptString("purge")
				purge := ok && purgeOpt == "yes"

				if purge {
					config, _ := getGuildConfig(db, event.GuildID().String())
					webhookID, _ := snowflake.Parse(config.WebhookID)

					msgs, _ := getMessagesToPurge(db, event.GuildID().String(), num)
					for _, msgIDStr := range msgs {
						if msgIDStr == "" {
							continue
						}
						msgID, _ := snowflake.Parse(msgIDStr)
						_ = event.Client().Rest.DeleteWebhookMessage(webhookID, config.WebhookToken, msgID, 0)
					}
					_ = deletePurgedMessages(db, event.GuildID().String(), num)
				}

				entry, _ := getCountEntry(db, event.GuildID().String())
				entry.Count = num
				entry.LastCounter = ""
				setCountEntry(db, entry)

				msg := fmt.Sprintf("Count set to %d.", num)
				if purge {
					msg += " Purged newer messages."
				}
				
				_, _ = event.Client().Rest.UpdateInteractionResponse(event.ApplicationID(), event.Token(), discord.MessageUpdate{
					Content: &msg,
				})
			case "channel":
				channelID := data.Snowflake("channel")
				config, _ := getGuildConfig(db, event.GuildID().String())

				webhook, err := createCountingWebhook(event.Client(), channelID)
				if err != nil {
					event.CreateMessage(discord.MessageCreate{
						Content: "Failed to create webhook in channel.",
						Flags:   discord.MessageFlagEphemeral,
					})
					return
				}

				config.Active = true
				config.Channel = channelID.String()
				config.WebhookID = webhook.ID().String()
				config.WebhookToken = webhook.(*discord.IncomingWebhook).Token
				setGuildConfig(db, config)
				event.CreateMessage(discord.MessageCreate{
					Content: fmt.Sprintf("Counting channel set to <#%s>", channelID.String()),
					Flags:   discord.MessageFlagEphemeral,
				})
			case "take-turns":
				state := data.String("state")
				config, _ := getGuildConfig(db, event.GuildID().String())
				if state == "on" {
					config.AllowDoublePost = false
				} else {
					config.AllowDoublePost = true
				}
				setGuildConfig(db, config)
				event.CreateMessage(discord.MessageCreate{
					Content: fmt.Sprintf("Take turns setting is now %s.", state),
					Flags:   discord.MessageFlagEphemeral,
				})
			case "view":
				config, _ := getGuildConfig(db, event.GuildID().String())

				channelStr := "None"
				if config.Channel != "" {
					channelStr = fmt.Sprintf("<#%s>", config.Channel)
				}

				takeTurns := "On"
				if config.AllowDoublePost {
					takeTurns = "Off"
				}

				out := fmt.Sprintf("**Tally Settings:**\n\n**Counting Channel:** %s\n**Take Turns:** %s", channelStr, takeTurns)

				event.CreateMessage(discord.MessageCreate{
					Content: out,
					Flags:   discord.MessageFlagEphemeral,
				})
			}
		}
	}
}
