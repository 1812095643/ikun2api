CREATE TABLE IF NOT EXISTS ops_monitor_observations (
 seq BIGSERIAL PRIMARY KEY, source TEXT NOT NULL, event_id TEXT NOT NULL,
 occurred BIGINT NOT NULL, body JSONB NOT NULL, UNIQUE(source,event_id)
);
CREATE INDEX IF NOT EXISTS ops_monitor_observations_time ON ops_monitor_observations(occurred DESC,seq DESC);
CREATE TABLE IF NOT EXISTS ops_monitor_task_facts (scope TEXT NOT NULL,kind TEXT NOT NULL,occurred BIGINT NOT NULL,body JSONB NOT NULL,PRIMARY KEY(scope,kind));
CREATE INDEX IF NOT EXISTS ops_monitor_task_facts_time ON ops_monitor_task_facts(occurred);
CREATE TABLE IF NOT EXISTS ops_monitor_source_cursors (source TEXT PRIMARY KEY,body JSONB NOT NULL);
CREATE TABLE IF NOT EXISTS ops_monitor_config (id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL,body JSONB NOT NULL);
CREATE TABLE IF NOT EXISTS ops_monitor_mutations (id TEXT PRIMARY KEY,actor TEXT NOT NULL,body JSONB NOT NULL,created BIGINT NOT NULL);
CREATE INDEX IF NOT EXISTS ops_monitor_mutations_time ON ops_monitor_mutations(created);
CREATE TABLE IF NOT EXISTS ops_monitor_incidents (
 id TEXT PRIMARY KEY,fingerprint TEXT NOT NULL UNIQUE,updated BIGINT NOT NULL,body JSONB NOT NULL
);
CREATE INDEX IF NOT EXISTS ops_monitor_incidents_updated ON ops_monitor_incidents(updated DESC,id);
CREATE INDEX IF NOT EXISTS ops_monitor_incidents_status ON ops_monitor_incidents((body->>'status'),updated DESC,id);
CREATE INDEX IF NOT EXISTS ops_monitor_incidents_group ON ops_monitor_incidents((body->>'group_id'),updated DESC,id);
CREATE INDEX IF NOT EXISTS ops_monitor_incidents_rule ON ops_monitor_incidents((body->>'rule_id'),updated DESC,id);
CREATE TABLE IF NOT EXISTS ops_monitor_incident_evidence (incident_id TEXT NOT NULL,evidence_id TEXT NOT NULL,created BIGINT NOT NULL,PRIMARY KEY(incident_id,evidence_id));
CREATE INDEX IF NOT EXISTS ops_monitor_evidence_time ON ops_monitor_incident_evidence(incident_id,created DESC);
CREATE TABLE IF NOT EXISTS ops_monitor_incident_impact (incident_id TEXT NOT NULL,kind TEXT NOT NULL,value TEXT NOT NULL,PRIMARY KEY(incident_id,kind,value));
CREATE TABLE IF NOT EXISTS ops_monitor_incident_transitions (id BIGSERIAL PRIMARY KEY,incident_id TEXT NOT NULL,created BIGINT NOT NULL,body JSONB NOT NULL);
CREATE INDEX IF NOT EXISTS ops_monitor_transitions_incident ON ops_monitor_incident_transitions(incident_id,id DESC);
CREATE TABLE IF NOT EXISTS ops_monitor_notification_outbox (
 id TEXT PRIMARY KEY,incident_id TEXT NOT NULL,revision INTEGER NOT NULL,channel TEXT NOT NULL,target TEXT NOT NULL,
 kind TEXT NOT NULL,state TEXT NOT NULL DEFAULT 'pending',next_attempt BIGINT NOT NULL,lease_until BIGINT NOT NULL DEFAULT 0,
 lease_token TEXT NOT NULL DEFAULT '',attempts INTEGER NOT NULL DEFAULT 0,send_started_at BIGINT NOT NULL DEFAULT 0,body JSONB NOT NULL,
 UNIQUE(incident_id,revision,channel,target,kind)
);
CREATE INDEX IF NOT EXISTS ops_monitor_outbox_ready ON ops_monitor_notification_outbox(next_attempt,lease_until) WHERE state IN ('pending','leased');
CREATE INDEX IF NOT EXISTS ops_monitor_outbox_order ON ops_monitor_notification_outbox(incident_id,channel,target,revision) WHERE state IN ('pending','leased');
CREATE INDEX IF NOT EXISTS ops_monitor_outbox_latest ON ops_monitor_notification_outbox(next_attempt DESC,id);
CREATE TABLE IF NOT EXISTS ops_monitor_message_map (incident_id TEXT NOT NULL,channel TEXT NOT NULL,target TEXT NOT NULL,message_id TEXT NOT NULL,revision INTEGER NOT NULL,PRIMARY KEY(incident_id,channel,target));
CREATE TABLE IF NOT EXISTS ops_monitor_rule_state (key TEXT PRIMARY KEY,body JSONB NOT NULL);

CREATE TABLE IF NOT EXISTS ops_monitor_channels (
 id TEXT PRIMARY KEY,kind TEXT NOT NULL,name TEXT NOT NULL,enabled BOOLEAN NOT NULL DEFAULT FALSE,
 settings JSONB NOT NULL DEFAULT '{}',secret_ciphertext TEXT NOT NULL DEFAULT '',secret_fields JSONB NOT NULL DEFAULT '[]',revision INTEGER NOT NULL DEFAULT 1,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),updated_by TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS ops_monitor_targets (
 id TEXT PRIMARY KEY,channel_id TEXT NOT NULL REFERENCES ops_monitor_channels(id) ON DELETE CASCADE,
 name TEXT NOT NULL,address TEXT NOT NULL,settings JSONB NOT NULL DEFAULT '{}',enabled BOOLEAN NOT NULL DEFAULT TRUE,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),UNIQUE(channel_id,address)
);

CREATE INDEX IF NOT EXISTS ops_monitor_incidents_platform ON ops_monitor_incidents((body->>'platform'),updated DESC,id);
CREATE INDEX IF NOT EXISTS ops_monitor_incidents_model ON ops_monitor_incidents((body->>'model'),updated DESC,id);
CREATE INDEX IF NOT EXISTS ops_monitor_incidents_account ON ops_monitor_incidents((body#>>'{sample,account_id}'),updated DESC,id);
CREATE INDEX IF NOT EXISTS ops_monitor_outbox_channel ON ops_monitor_notification_outbox(channel,next_attempt DESC,id);

CREATE INDEX IF NOT EXISTS ops_monitor_incidents_scope ON ops_monitor_incidents((body->>'scope')) WHERE body->>'status' NOT IN ('resolved','closed','superseded');
