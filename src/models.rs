pub struct GuildConfig {
    pub id: String,
    pub active: bool,
    pub channel: String,
    pub webhook_id: String,
    pub webhook_token: String,
    pub allow_double_post: bool,
}

pub struct CountEntry {
    pub guild: String,
    pub last_counter: String,
    pub count: i64,
}

pub struct GuildRule {
    pub id: String,
    pub guild: String,
    pub trigger: String,
    pub rule_type: String,
    pub value: i64,
    pub action: String,
    pub action_v1: String,
}
