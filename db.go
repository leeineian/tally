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

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS guild_configs (
			id TEXT PRIMARY KEY,
			active BOOLEAN,
			channel TEXT,
			webhook_id TEXT,
			webhook_token TEXT,
			allow_double_post BOOLEAN
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
	`)
	if err != nil {
		panic(err)
	}
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
	return rules, nil
}
