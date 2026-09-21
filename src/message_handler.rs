use std::sync::{Arc, Mutex};
use rusqlite::Connection;
use twilight_http::Client as HttpClient;
use twilight_model::{
    channel::message::Message,
    id::{marker::WebhookMarker, Id},
};
use crate::database;

pub async fn handle_message(
    db: Arc<Mutex<Connection>>,
    http: Arc<HttpClient>,
    msg: Box<Message>,
) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    if msg.author.bot || msg.guild_id.is_none() {
        return Ok(());
    }

    let guild_id = msg.guild_id.unwrap().get().to_string();
    
    let config = {
        let conn = db.lock().unwrap();
        database::get_guild_config(&conn, &guild_id)?
    };

    if !config.active || config.channel != msg.channel_id.get().to_string() {
        return Ok(());
    }

    let content: String = msg.content.chars().filter(|c| c.is_ascii_digit()).collect();
    let provided_int = if let Ok(val) = content.parse::<i64>() {
        val
    } else {
        return Ok(());
    };

    let mut entry = {
        let conn = db.lock().unwrap();
        database::get_count_entry(&conn, &guild_id)?
    };

    let next_count = entry.count + 1;

    if provided_int != next_count {
        let _ = http.delete_message(msg.channel_id, msg.id).await;
        return Ok(());
    }

    let author_id = msg.author.id.get().to_string();
    if entry.last_counter == author_id && !config.allow_double_post {
        let _ = http.delete_message(msg.channel_id, msg.id).await;
        if let Ok(dm_channel) = http.create_private_channel(msg.author.id).await {
            if let Ok(channel) = dm_channel.model().await {
                let _ = http.create_message(channel.id)
                    .content(&format!("You have already counted in <#{}>! Wait for someone else to count before you count again.", msg.channel_id))
                    .await;
            }
        }
        return Ok(());
    }

    let _ = http.delete_message(msg.channel_id, msg.id).await;

    entry.count = next_count;
    entry.last_counter = author_id.clone();
    {
        let conn = db.lock().unwrap();
        database::set_count_entry(&conn, &entry)?;
    }

    let webhook_id = Id::<WebhookMarker>::new(config.webhook_id.parse().unwrap_or(1));
    let webhook_token = &config.webhook_token;

    let avatar_url = if let Some(hash) = &msg.author.avatar {
        format!("https://cdn.discordapp.com/avatars/{}/{}.png", author_id, hash)
    } else {
        "".to_string()
    };

    let exec_res = http.execute_webhook(webhook_id, webhook_token)
        .content(&next_count.to_string())
        .username(&msg.author.name)
        .avatar_url(&avatar_url)
        .wait()
        .await;

    let mut api_msg_id = None;
    match exec_res {
        Ok(resp) => {
            if let Ok(message) = resp.model().await {
                api_msg_id = Some(message.id);
            }
        },
        Err(_) => {
            // Error handling ignored for brevity
        }
    }

    if let Some(msg_id) = api_msg_id {
        let conn = db.lock().unwrap();
        database::save_count_message(&conn, &guild_id, next_count, &msg_id.get().to_string())?;
    }

    let rules = {
        let conn = db.lock().unwrap();
        database::get_guild_rules(&conn, &guild_id)?
    };

    for rule in rules {
        let mut match_rule = false;
        if rule.rule_type == "equals" && next_count == rule.value {
            match_rule = true;
        } else if rule.rule_type == "multiple_of" && rule.value != 0 && next_count % rule.value == 0 {
            match_rule = true;
        }

        if !match_rule {
            continue;
        }

        let rule_content = rule.action_v1.replace("{{count}}", &next_count.to_string());

        match rule.action.as_str() {
            "pin" => {
                if let Some(msg_id) = api_msg_id {
                    let _ = http.create_pin(msg.channel_id, msg_id).await;
                }
            }
            "dm" => {
                if let Ok(dm_channel) = http.create_private_channel(msg.author.id).await {
                    if let Ok(channel) = dm_channel.model().await {
                        let _ = http.create_message(channel.id).content(&rule_content).await;
                    }
                }
            }
            "msg" => {
                let _ = http.create_message(msg.channel_id).content(&rule_content).await;
            }
            _ => {}
        }
    }

    Ok(())
}
