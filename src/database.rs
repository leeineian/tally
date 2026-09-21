use rusqlite::{Connection, Result};
use crate::models::{GuildConfig, CountEntry, GuildRule};

pub fn init_db() -> Connection {
    let db_url = std::env::var("DATABASE_URL").unwrap_or_else(|_| "tally.db".to_string());
    let conn = Connection::open(db_url).expect("Failed to open database");

    let _ = conn.pragma_update(None, "journal_mode", "WAL");
    let _ = conn.pragma_update(None, "busy_timeout", 5000);
    let _ = conn.pragma_update(None, "synchronous", "NORMAL");

    conn.execute_batch(
        r#"
        CREATE TABLE IF NOT EXISTS guild_configs (
            id TEXT PRIMARY KEY,
            active BOOLEAN,
            channel TEXT,
            webhook_id TEXT,
            webhook_token TEXT,
            allow_double_post BOOLEAN,
            live_status BOOLEAN,
            live_count_id TEXT,
            live_count_content TEXT
        );
        CREATE TABLE IF NOT EXISTS count_entries (
            guild TEXT PRIMARY KEY,
            last_counter TEXT,
            count INTEGER
        );
        CREATE TABLE IF NOT EXISTS guild_rules (
            id TEXT PRIMARY KEY,
            guild TEXT,
            trigger TEXT,
            type TEXT,
            value INTEGER,
            action TEXT,
            action_v1 TEXT
        );
        CREATE TABLE IF NOT EXISTS count_messages (
            guild TEXT,
            count INTEGER,
            message_id TEXT,
            PRIMARY KEY (guild, count)
        );
        "#
    ).unwrap();

    let _ = conn.execute("ALTER TABLE guild_configs ADD COLUMN live_status BOOLEAN DEFAULT 0;", []);
    let _ = conn.execute("ALTER TABLE guild_configs ADD COLUMN live_count_id TEXT DEFAULT '';", []);
    let _ = conn.execute("ALTER TABLE guild_configs ADD COLUMN live_count_content TEXT DEFAULT '';", []);

    conn
}

pub fn get_guild_config(conn: &Connection, guild_id: &str) -> Result<GuildConfig> {
    let mut stmt = conn.prepare("SELECT id, active, channel, webhook_id, webhook_token, allow_double_post FROM guild_configs WHERE id = ?")?;
    let mut rows = stmt.query([guild_id])?;

    if let Some(row) = rows.next()? {
        Ok(GuildConfig {
            id: row.get(0)?,
            active: row.get(1)?,
            channel: row.get(2)?,
            webhook_id: row.get(3)?,
            webhook_token: row.get(4)?,
            allow_double_post: row.get(5)?,
        })
    } else {
        Ok(GuildConfig {
            id: guild_id.to_string(),
            active: false,
            channel: String::new(),
            webhook_id: String::new(),
            webhook_token: String::new(),
            allow_double_post: false,
        })
    }
}

pub fn set_guild_config(conn: &Connection, c: &GuildConfig) -> Result<()> {
    conn.execute(
        r#"
        INSERT INTO guild_configs (id, active, channel, webhook_id, webhook_token, allow_double_post)
        VALUES (?1, ?2, ?3, ?4, ?5, ?6)
        ON CONFLICT(id) DO UPDATE SET
            active = excluded.active,
            channel = excluded.channel,
            webhook_id = excluded.webhook_id,
            webhook_token = excluded.webhook_token,
            allow_double_post = excluded.allow_double_post
        "#,
        (
            &c.id,
            &c.active,
            &c.channel,
            &c.webhook_id,
            &c.webhook_token,
            &c.allow_double_post,
        ),
    )?;
    Ok(())
}

pub fn get_count_entry(conn: &Connection, guild_id: &str) -> Result<CountEntry> {
    let mut stmt = conn.prepare("SELECT guild, last_counter, count FROM count_entries WHERE guild = ?")?;
    let mut rows = stmt.query([guild_id])?;

    if let Some(row) = rows.next()? {
        Ok(CountEntry {
            guild: row.get(0)?,
            last_counter: row.get(1)?,
            count: row.get(2)?,
        })
    } else {
        Ok(CountEntry {
            guild: guild_id.to_string(),
            last_counter: String::new(),
            count: 0,
        })
    }
}

pub fn set_count_entry(conn: &Connection, c: &CountEntry) -> Result<()> {
    conn.execute(
        r#"
        INSERT INTO count_entries (guild, last_counter, count)
        VALUES (?1, ?2, ?3)
        ON CONFLICT(guild) DO UPDATE SET
            last_counter = excluded.last_counter,
            count = excluded.count
        "#,
        (&c.guild, &c.last_counter, &c.count),
    )?;
    Ok(())
}

pub fn get_guild_rules(conn: &Connection, guild_id: &str) -> Result<Vec<GuildRule>> {
    let mut stmt = conn.prepare("SELECT id, guild, trigger, type, value, action, action_v1 FROM guild_rules WHERE guild = ?")?;
    let rules = stmt.query_map([guild_id], |row| {
        Ok(GuildRule {
            id: row.get(0)?,
            guild: row.get(1)?,
            trigger: row.get(2)?,
            rule_type: row.get(3)?,
            value: row.get(4)?,
            action: row.get(5)?,
            action_v1: row.get(6)?,
        })
    })?;

    let mut result = Vec::new();
    for rule in rules {
        result.push(rule?);
    }
    Ok(result)
}

pub fn add_guild_rule(conn: &Connection, r: &GuildRule) -> Result<()> {
    conn.execute(
        r#"
        INSERT INTO guild_rules (id, guild, trigger, type, value, action, action_v1)
        VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7)
        "#,
        (
            &r.id,
            &r.guild,
            &r.trigger,
            &r.rule_type,
            &r.value,
            &r.action,
            &r.action_v1,
        ),
    )?;
    Ok(())
}

pub fn delete_guild_rule(conn: &Connection, id: &str, guild_id: &str) -> Result<()> {
    conn.execute(
        "DELETE FROM guild_rules WHERE id = ? AND guild = ?",
        (id, guild_id),
    )?;
    Ok(())
}

pub fn get_guild_rule(conn: &Connection, id: &str, guild_id: &str) -> Result<GuildRule> {
    conn.query_row(
        "SELECT id, guild, trigger, type, value, action, action_v1 FROM guild_rules WHERE id = ? AND guild = ?",
        (id, guild_id),
        |row| {
            Ok(GuildRule {
                id: row.get(0)?,
                guild: row.get(1)?,
                trigger: row.get(2)?,
                rule_type: row.get(3)?,
                value: row.get(4)?,
                action: row.get(5)?,
                action_v1: row.get(6)?,
            })
        },
    )
}

pub fn save_count_message(conn: &Connection, guild_id: &str, count: i64, message_id: &str) -> Result<()> {
    conn.execute(
        r#"
        INSERT INTO count_messages (guild, count, message_id)
        VALUES (?1, ?2, ?3)
        ON CONFLICT(guild, count) DO UPDATE SET message_id = excluded.message_id
        "#,
        (guild_id, count, message_id),
    )?;
    Ok(())
}

pub fn get_messages_to_purge(conn: &Connection, guild_id: &str, target_count: i64) -> Result<Vec<String>> {
    let mut stmt = conn.prepare("SELECT message_id FROM count_messages WHERE guild = ? AND count > ?")?;
    let msgs = stmt.query_map((guild_id, target_count), |row| row.get(0))?;

    let mut result = Vec::new();
    for msg in msgs {
        result.push(msg?);
    }
    Ok(result)
}

pub fn delete_purged_messages(conn: &Connection, guild_id: &str, target_count: i64) -> Result<()> {
    conn.execute(
        "DELETE FROM count_messages WHERE guild = ? AND count > ?",
        (guild_id, target_count),
    )?;
    Ok(())
}
