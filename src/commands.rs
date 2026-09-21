use twilight_model::{
    application::command::{Command, CommandType},
    channel::ChannelType,
    application::interaction::{Interaction, InteractionData},
    application::interaction::application_command::CommandOptionValue,
    http::interaction::{InteractionResponse, InteractionResponseData, InteractionResponseType},
    id::marker::WebhookMarker,
    id::Id,
};
use twilight_util::builder::command::{
    ChannelBuilder, CommandBuilder, IntegerBuilder, StringBuilder, SubCommandBuilder,
};
use std::sync::{Arc, Mutex};
use rusqlite::Connection;
use twilight_http::Client as HttpClient;

pub fn get_commands() -> Vec<Command> {
    vec![
        CommandBuilder::new("about", "Information about the bot", CommandType::ChatInput).build(),
        CommandBuilder::new("current-count", "Get the current count", CommandType::ChatInput).build(),
        CommandBuilder::new("rule", "Manage counting rules", CommandType::ChatInput)
            .option(
                SubCommandBuilder::new("create", "Create a new rule")
                    .option(
                        StringBuilder::new("action", "Action to perform")
                            .required(true)
                            .choices(vec![
                                ("Pin Message".to_string(), "pin".to_string()),
                                ("Send DM".to_string(), "dm".to_string()),
                                ("Send Message".to_string(), "msg".to_string()),
                            ])
                    )
                    .option(
                        StringBuilder::new("type", "Condition type")
                            .required(true)
                            .choices(vec![
                                ("Equals".to_string(), "equals".to_string()),
                                ("Multiple Of".to_string(), "multiple_of".to_string()),
                            ])
                    )
                    .option(IntegerBuilder::new("value", "Value for condition").required(true))
                    .option(StringBuilder::new("content", "Content to send (use {{count}} for count) (ignored for pin)").required(false))
            )
            .option(
                SubCommandBuilder::new("delete", "Delete a specific rule")
                    .option(StringBuilder::new("id", "Rule ID").required(true))
            )
            .option(
                SubCommandBuilder::new("view", "View a specific rule")
                    .option(StringBuilder::new("id", "Rule ID").required(true))
            )
            .option(SubCommandBuilder::new("list", "List all rules"))
            .build(),
        CommandBuilder::new("settings", "Configure tally settings", CommandType::ChatInput)
            .option(
                SubCommandBuilder::new("current-count", "Set the current count")
                    .option(IntegerBuilder::new("number", "The count to set").required(true))
                    .option(
                        StringBuilder::new("purge", "Purge messages down to this count?")
                            .required(false)
                            .choices(vec![
                                ("Yes".to_string(), "yes".to_string()),
                                ("No".to_string(), "no".to_string()),
                            ])
                    )
            )
            .option(
                SubCommandBuilder::new("channel", "Set the counting channel")
                    .option(
                        ChannelBuilder::new("channel", "The channel to count in")
                            .required(true)
                            .channel_types(vec![ChannelType::GuildText])
                    )
            )
            .option(
                SubCommandBuilder::new("take-turns", "Toggle consecutive counting")
                    .option(
                        StringBuilder::new("state", "State (on forces taking turns, off allows consecutive counting)")
                            .required(true)
                            .choices(vec![
                                ("On".to_string(), "on".to_string()),
                                ("Off".to_string(), "off".to_string()),
                            ])
                    )
            )
            .option(SubCommandBuilder::new("view", "View current settings"))
            .build(),
    ]
}

pub async fn handle_interaction(
    db: Arc<Mutex<Connection>>,
    http: Arc<HttpClient>,
    interaction: Box<Interaction>,
) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    let app_id = interaction.application_id;
    let interaction_id = interaction.id;
    let interaction_token = interaction.token.clone();
    
    if let Some(InteractionData::ApplicationCommand(data)) = &interaction.data {
        let response_data = match data.name.as_str() {
            "about" => {
                Some(InteractionResponseData {
                    content: Some(format!("Tally counting bot rewritten in Rust.")),
                    flags: Some(twilight_model::channel::message::MessageFlags::EPHEMERAL),
                    ..Default::default()
                })
            }
            "current-count" => {
                if let Some(guild_id) = interaction.guild_id {
                    let entry = {
                        let conn = db.lock().unwrap();
                        crate::database::get_count_entry(&conn, &guild_id.get().to_string()).unwrap()
                    };
                    Some(InteractionResponseData {
                        content: Some(format!("The current count is {}", entry.count)),
                        flags: Some(twilight_model::channel::message::MessageFlags::EPHEMERAL),
                        ..Default::default()
                    })
                } else {
                    None
                }
            }
            "rule" => {
                if let Some(guild_id) = interaction.guild_id {
                    if let Some(opt) = data.options.first() {
                        match &opt.value {
                            CommandOptionValue::SubCommand(opts) => {
                                match opt.name.as_str() {
                                    "create" => {
                                        let mut action = String::new();
                                        let mut typ = String::new();
                                        let mut val = 0;
                                        let mut content = String::new();
                                        for o in opts {
                                            match o.name.as_str() {
                                                "action" => if let CommandOptionValue::String(s) = &o.value { action = s.clone(); },
                                                "type" => if let CommandOptionValue::String(s) = &o.value { typ = s.clone(); },
                                                "value" => if let CommandOptionValue::Integer(i) = &o.value { val = *i; },
                                                "content" => if let CommandOptionValue::String(s) = &o.value { content = s.clone(); },
                                                _ => {}
                                            }
                                        }
                                        if content.is_empty() && action != "pin" {
                                            content = "Triggered rule at count {{count}}!".to_string();
                                        }
                                        let id = format!("{:x}", interaction.id.get());
                                        let r = crate::models::GuildRule {
                                            id: id.clone(),
                                            guild: guild_id.get().to_string(),
                                            trigger: "count".to_string(),
                                            rule_type: typ.clone(),
                                            value: val,
                                            action: action.clone(),
                                            action_v1: content,
                                        };
                                        {
                                            let conn = db.lock().unwrap();
                                            let _ = crate::database::add_guild_rule(&conn, &r);
                                        }
                                        Some(InteractionResponseData {
                                            content: Some(format!("Created rule `{}`:\nAction: {}\nCondition: {} {}", id, action, typ, val)),
                                            flags: Some(twilight_model::channel::message::MessageFlags::EPHEMERAL),
                                            ..Default::default()
                                        })
                                    }
                                    "delete" => {
                                        let mut id = String::new();
                                        for o in opts {
                                            if o.name == "id" {
                                                if let CommandOptionValue::String(s) = &o.value { id = s.clone(); }
                                            }
                                        }
                                        {
                                            let conn = db.lock().unwrap();
                                            let _ = crate::database::delete_guild_rule(&conn, &id, &guild_id.get().to_string());
                                        }
                                        Some(InteractionResponseData {
                                            content: Some(format!("Deleted rule `{}`", id)),
                                            flags: Some(twilight_model::channel::message::MessageFlags::EPHEMERAL),
                                            ..Default::default()
                                        })
                                    }
                                    "view" => {
                                        let mut id = String::new();
                                        for o in opts {
                                            if o.name == "id" {
                                                if let CommandOptionValue::String(s) = &o.value { id = s.clone(); }
                                            }
                                        }
                                        let rule = {
                                            let conn = db.lock().unwrap();
                                            crate::database::get_guild_rule(&conn, &id, &guild_id.get().to_string())
                                        };
                                        if let Ok(r) = rule {
                                            Some(InteractionResponseData {
                                                content: Some(format!("Rule `{}`:\nAction: {}\nType: {}\nValue: {}\nContent: {}", r.id, r.action, r.rule_type, r.value, r.action_v1)),
                                                flags: Some(twilight_model::channel::message::MessageFlags::EPHEMERAL),
                                                ..Default::default()
                                            })
                                        } else {
                                            Some(InteractionResponseData {
                                                content: Some("Rule not found.".to_string()),
                                                flags: Some(twilight_model::channel::message::MessageFlags::EPHEMERAL),
                                                ..Default::default()
                                            })
                                        }
                                    }
                                    "list" => {
                                        let rules = {
                                            let conn = db.lock().unwrap();
                                            crate::database::get_guild_rules(&conn, &guild_id.get().to_string()).unwrap_or_default()
                                        };
                                        if rules.is_empty() {
                                            Some(InteractionResponseData {
                                                content: Some("No rules configured.".to_string()),
                                                flags: Some(twilight_model::channel::message::MessageFlags::EPHEMERAL),
                                                ..Default::default()
                                            })
                                        } else {
                                            let mut out = "Rules:\n".to_string();
                                            for r in rules {
                                                out.push_str(&format!("- `{}`: {} on {} {}\n", r.id, r.action, r.rule_type, r.value));
                                            }
                                            Some(InteractionResponseData {
                                                content: Some(out),
                                                flags: Some(twilight_model::channel::message::MessageFlags::EPHEMERAL),
                                                ..Default::default()
                                            })
                                        }
                                    }
                                    _ => None
                                }
                            }
                            _ => None
                        }
                    } else {
                        None
                    }
                } else {
                    None
                }
            }
            "settings" => {
                if let Some(guild_id) = interaction.guild_id {
                    if let Some(opt) = data.options.first() {
                        match &opt.value {
                            CommandOptionValue::SubCommand(opts) => {
                                match opt.name.as_str() {
                                    "current-count" => {
                                        let mut num = 0;
                                        let mut purge = false;
                                        for o in opts {
                                            match o.name.as_str() {
                                                "number" => if let CommandOptionValue::Integer(i) = &o.value { num = *i; },
                                                "purge" => if let CommandOptionValue::String(s) = &o.value { purge = s == "yes"; },
                                                _ => {}
                                            }
                                        }
                                        if purge {
                                            let config = {
                                                let conn = db.lock().unwrap();
                                                crate::database::get_guild_config(&conn, &guild_id.get().to_string()).unwrap()
                                            };
                                            let webhook_id_parsed: u64 = config.webhook_id.parse().unwrap_or(0);
                                            if webhook_id_parsed != 0 {
                                                let webhook_id = Id::<WebhookMarker>::new(webhook_id_parsed);
                                                let msgs = {
                                                    let conn = db.lock().unwrap();
                                                    crate::database::get_messages_to_purge(&conn, &guild_id.get().to_string(), num).unwrap_or_default()
                                                };
                                                for msg_id_str in msgs {
                                                    if let Ok(m_id) = msg_id_str.parse::<u64>() {
                                                        let _ = http.delete_webhook_message(webhook_id, &config.webhook_token, Id::new(m_id)).await;
                                                    }
                                                }
                                                {
                                                    let conn = db.lock().unwrap();
                                                    let _ = crate::database::delete_purged_messages(&conn, &guild_id.get().to_string(), num);
                                                }
                                            }
                                        }

                                        {
                                            let conn = db.lock().unwrap();
                                            let mut entry = crate::database::get_count_entry(&conn, &guild_id.get().to_string()).unwrap();
                                            entry.count = num;
                                            entry.last_counter = "".to_string();
                                            let _ = crate::database::set_count_entry(&conn, &entry);
                                        }

                                        let mut msg = format!("Count set to {}.", num);
                                        if purge {
                                            msg.push_str(" Purged newer messages.");
                                        }

                                        Some(InteractionResponseData {
                                            content: Some(msg),
                                            flags: Some(twilight_model::channel::message::MessageFlags::EPHEMERAL),
                                            ..Default::default()
                                        })
                                    }
                                    "channel" => {
                                        let mut channel_id = None;
                                        for o in opts {
                                            if o.name == "channel" {
                                                if let CommandOptionValue::Channel(c) = &o.value { channel_id = Some(*c); }
                                            }
                                        }
                                        if let Some(cid) = channel_id {
                                            if let Ok(wh_res) = http.create_webhook(cid, "Tally Counting").await {
                                                if let Ok(webhook) = wh_res.model().await {
                                                    let mut config = {
                                                        let conn = db.lock().unwrap();
                                                        crate::database::get_guild_config(&conn, &guild_id.get().to_string()).unwrap()
                                                    };
                                                    config.active = true;
                                                    config.channel = cid.get().to_string();
                                                    config.webhook_id = webhook.id.get().to_string();
                                                    config.webhook_token = webhook.token.unwrap_or_default();
                                                    {
                                                        let conn = db.lock().unwrap();
                                                        let _ = crate::database::set_guild_config(&conn, &config);
                                                    }
                                                    Some(InteractionResponseData {
                                                        content: Some(format!("Counting channel set to <#{}>", cid.get())),
                                                        flags: Some(twilight_model::channel::message::MessageFlags::EPHEMERAL),
                                                        ..Default::default()
                                                    })
                                                } else {
                                                    Some(InteractionResponseData {
                                                        content: Some("Failed to get webhook model.".to_string()),
                                                        flags: Some(twilight_model::channel::message::MessageFlags::EPHEMERAL),
                                                        ..Default::default()
                                                    })
                                                }
                                            } else {
                                                Some(InteractionResponseData {
                                                    content: Some("Failed to create webhook in channel.".to_string()),
                                                    flags: Some(twilight_model::channel::message::MessageFlags::EPHEMERAL),
                                                    ..Default::default()
                                                })
                                            }
                                        } else {
                                            None
                                        }
                                    }
                                    "take-turns" => {
                                        let mut state = String::new();
                                        for o in opts {
                                            if o.name == "state" {
                                                if let CommandOptionValue::String(s) = &o.value { state = s.clone(); }
                                            }
                                        }
                                        let mut config = {
                                            let conn = db.lock().unwrap();
                                            crate::database::get_guild_config(&conn, &guild_id.get().to_string()).unwrap()
                                        };
                                        if state == "on" {
                                            config.allow_double_post = false;
                                        } else {
                                            config.allow_double_post = true;
                                        }
                                        {
                                            let conn = db.lock().unwrap();
                                            let _ = crate::database::set_guild_config(&conn, &config);
                                        }
                                        Some(InteractionResponseData {
                                            content: Some(format!("Take turns setting is now {}.", state)),
                                            flags: Some(twilight_model::channel::message::MessageFlags::EPHEMERAL),
                                            ..Default::default()
                                        })
                                    }
                                    "view" => {
                                        let config = {
                                            let conn = db.lock().unwrap();
                                            crate::database::get_guild_config(&conn, &guild_id.get().to_string()).unwrap()
                                        };
                                        let channel_str = if config.channel.is_empty() {
                                            "None".to_string()
                                        } else {
                                            format!("<#{}>", config.channel)
                                        };
                                        let take_turns = if config.allow_double_post { "Off" } else { "On" };
                                        Some(InteractionResponseData {
                                            content: Some(format!("**Tally Settings:**\n\n**Counting Channel:** {}\n**Take Turns:** {}", channel_str, take_turns)),
                                            flags: Some(twilight_model::channel::message::MessageFlags::EPHEMERAL),
                                            ..Default::default()
                                        })
                                    }
                                    _ => None
                                }
                            }
                            _ => None
                        }
                    } else {
                        None
                    }
                } else {
                    None
                }
            }
            _ => None
        };

        if let Some(data) = response_data {
            let response = InteractionResponse {
                kind: InteractionResponseType::ChannelMessageWithSource,
                data: Some(data),
            };
            http.interaction(app_id)
                .create_response(interaction_id, &interaction_token, &response)
                .await?;
        }
    }
    
    Ok(())
}
