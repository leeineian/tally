mod models;
mod database;
mod commands;
mod message_handler;

use clap::Parser;
use dotenvy::dotenv;
use std::env;
use std::sync::{Arc, Mutex};
use twilight_gateway::{CloseFrame, Event, EventTypeFlags, Intents, Shard, ShardId, StreamExt};
use twilight_http::Client as HttpClient;
use twilight_model::id::Id;

#[derive(Parser, Debug)]
#[command(author, version, about, long_about = None)]
struct Args {
    #[arg(long, default_value_t = false)]
    cleanup: bool,

    #[arg(long, default_value_t = false)]
    refresh: bool,

    #[arg(long)]
    guild: Option<String>,
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    let args = Args::parse();
    let _ = dotenv();
    
    let _ = rustls::crypto::ring::default_provider().install_default();

    let token = env::var("BOT_TOKEN").expect("BOT_TOKEN is not set");
    let http = Arc::new(HttpClient::new(token.clone()));

    let db = Arc::new(Mutex::new(database::init_db()));

    let current_user = http.current_user().await?.model().await?;
    let application_id = http.current_user_application().await?.model().await?.id;

    if args.cleanup {
        if let Some(ref guild_id_str) = args.guild {
            if let Ok(g_id) = guild_id_str.parse::<u64>() {
                let guild_id = Id::new(g_id);
                http.interaction(application_id).set_guild_commands(guild_id, &[]).await?;
                println!("Cleaned up guild commands for {}", g_id);
            }
        } else {
            http.interaction(application_id).set_global_commands(&[]).await?;
            println!("Cleaned up global commands.");
        }
        return Ok(());
    }

    if args.refresh {
        let commands = commands::get_commands();
        if let Some(ref guild_id_str) = args.guild {
            if let Ok(g_id) = guild_id_str.parse::<u64>() {
                let guild_id = Id::new(g_id);
                http.interaction(application_id).set_guild_commands(guild_id, &commands).await?;
                println!("Refreshed guild commands for {}", g_id);
            }
        } else {
            http.interaction(application_id).set_global_commands(&commands).await?;
            println!("Refreshed global commands.");
        }
    }

    println!("{} ({}) is up. Press CTRL-C to exit.", current_user.name, application_id);

    let intents = Intents::GUILDS | Intents::GUILD_MESSAGES | Intents::MESSAGE_CONTENT;
    let mut shard = Shard::new(ShardId::ONE, token, intents);

    loop {
        tokio::select! {
            event_res = shard.next_event(EventTypeFlags::all()) => {
                let event = match event_res {
                    Some(Ok(event)) => event,
                    Some(Err(source)) => {
                        println!("Error receiving event: {:?}", source);
                        continue;
                    }
                    None => break,
                };

                let db_clone = Arc::clone(&db);
                let http_clone = Arc::clone(&http);
                
                match event {
                    Event::InteractionCreate(interaction) => {
                        tokio::spawn(async move {
                            if let Err(e) = commands::handle_interaction(db_clone, http_clone, Box::new(interaction.0)).await {
                                println!("Error handling interaction: {}", e);
                            }
                        });
                    }
                    Event::MessageCreate(msg) => {
                        tokio::spawn(async move {
                            if let Err(e) = message_handler::handle_message(db_clone, http_clone, Box::new(msg.0)).await {
                                println!("Error handling message: {}", e);
                            }
                        });
                    }
                    _ => {}
                }
            }
            _ = tokio::signal::ctrl_c() => {
                println!("Ctrl-C received, shutting down gracefully...");
                let _ = shard.close(CloseFrame::NORMAL);
                let _ = tokio::time::timeout(std::time::Duration::from_millis(1500), async {
                    while let Some(_) = shard.next_event(EventTypeFlags::all()).await {}
                }).await;
                
                break;
            }
        }
    }

    Ok(())
}
