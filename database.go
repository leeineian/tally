package main

import (
	"database/sql"
	"os"

	_ "modernc.org/sqlite"
)

type GuildConfig struct {
	ID              string
	Active          bool
	Channel         string
	WebhookID       string
	WebhookToken    string
	AllowDoublePost bool
}

type CountEntry struct {
	Guild       string
	LastCounter string
	Count       int
}

type GuildRule struct {
	ID       string
	Guild    string
	Trigger  string
	Type     string
	Value    int
	Action   string
	ActionV1 string
}

func initDB() *sql.DB {
	db, err := sql.Open("sqlite", os.Getenv("DATABASE_URL"))
	if err != nil {
		panic(err)
	}

	_, _ = db.Exec("PRAGMA journal_mode=WAL;")
	_, _ = db.Exec("PRAGMA busy_timeout=5000;")
	_, _ = db.Exec("PRAGMA synchronous=NORMAL;")

	_, err = db.Exec(`
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
	`)
	if err != nil {
		panic(err)
	}
	
	_, _ = db.Exec("ALTER TABLE guild_configs ADD COLUMN live_status BOOLEAN DEFAULT 0;")
	_, _ = db.Exec("ALTER TABLE guild_configs ADD COLUMN live_count_id TEXT DEFAULT '';")
	_, _ = db.Exec("ALTER TABLE guild_configs ADD COLUMN live_count_content TEXT DEFAULT '';")

	return db
}

func getGuildConfig(db *sql.DB, guildID string) (GuildConfig, error) {
	var c GuildConfig
	err := db.QueryRow("SELECT id, active, channel, webhook_id, webhook_token, allow_double_post FROM guild_configs WHERE id = ?", guildID).
		Scan(&c.ID, &c.Active, &c.Channel, &c.WebhookID, &c.WebhookToken, &c.AllowDoublePost)
	if err == sql.ErrNoRows {
		return GuildConfig{ID: guildID}, nil
	}
	return c, err
}

func setGuildConfig(db *sql.DB, c GuildConfig) error {
	_, err := db.Exec(`
		INSERT INTO guild_configs (id, active, channel, webhook_id, webhook_token, allow_double_post)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			active = excluded.active,
			channel = excluded.channel,
			webhook_id = excluded.webhook_id,
			webhook_token = excluded.webhook_token,
			allow_double_post = excluded.allow_double_post
	`, c.ID, c.Active, c.Channel, c.WebhookID, c.WebhookToken, c.AllowDoublePost)
	return err
}

func getCountEntry(db *sql.DB, guildID string) (CountEntry, error) {
	var c CountEntry
	err := db.QueryRow("SELECT guild, last_counter, count FROM count_entries WHERE guild = ?", guildID).
		Scan(&c.Guild, &c.LastCounter, &c.Count)
	if err == sql.ErrNoRows {
		return CountEntry{Guild: guildID, Count: 0}, nil
	}
	return c, err
}

func setCountEntry(db *sql.DB, c CountEntry) error {
	_, err := db.Exec(`
		INSERT INTO count_entries (guild, last_counter, count)
		VALUES (?, ?, ?)
		ON CONFLICT(guild) DO UPDATE SET
			last_counter = excluded.last_counter,
			count = excluded.count
	`, c.Guild, c.LastCounter, c.Count)
	return err
}

func getGuildRules(db *sql.DB, guildID string) ([]GuildRule, error) {
	rows, err := db.Query("SELECT id, guild, trigger, type, value, action, action_v1 FROM guild_rules WHERE guild = ?", guildID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []GuildRule
	for rows.Next() {
		var r GuildRule
		if err := rows.Scan(&r.ID, &r.Guild, &r.Trigger, &r.Type, &r.Value, &r.Action, &r.ActionV1); err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return rules, nil
}

func addGuildRule(db *sql.DB, r GuildRule) error {
	_, err := db.Exec(`
		INSERT INTO guild_rules (id, guild, trigger, type, value, action, action_v1)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, r.ID, r.Guild, r.Trigger, r.Type, r.Value, r.Action, r.ActionV1)
	return err
}

func deleteGuildRule(db *sql.DB, id, guildID string) error {
	_, err := db.Exec("DELETE FROM guild_rules WHERE id = ? AND guild = ?", id, guildID)
	return err
}

func getGuildRule(db *sql.DB, id, guildID string) (GuildRule, error) {
	var r GuildRule
	err := db.QueryRow("SELECT id, guild, trigger, type, value, action, action_v1 FROM guild_rules WHERE id = ? AND guild = ?", id, guildID).
		Scan(&r.ID, &r.Guild, &r.Trigger, &r.Type, &r.Value, &r.Action, &r.ActionV1)
	return r, err
}

func saveCountMessage(db *sql.DB, guildID string, count int, messageID string) error {
	_, err := db.Exec(`
		INSERT INTO count_messages (guild, count, message_id)
		VALUES (?, ?, ?)
		ON CONFLICT(guild, count) DO UPDATE SET message_id = excluded.message_id
	`, guildID, count, messageID)
	return err
}

func getMessagesToPurge(db *sql.DB, guildID string, targetCount int) ([]string, error) {
	rows, err := db.Query("SELECT message_id FROM count_messages WHERE guild = ? AND count > ?", guildID, targetCount)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			msgs = append(msgs, id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return msgs, nil
}

func deletePurgedMessages(db *sql.DB, guildID string, targetCount int) error {
	_, err := db.Exec("DELETE FROM count_messages WHERE guild = ? AND count > ?", guildID, targetCount)
	return err
}
