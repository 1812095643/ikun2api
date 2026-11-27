CREATE TABLE IF NOT EXISTS ops_monitor_request_outcomes (
 request_id TEXT PRIMARY KEY, occurred BIGINT NOT NULL, body JSONB NOT NULL
);
CREATE INDEX IF NOT EXISTS ops_monitor_request_outcomes_time ON ops_monitor_request_outcomes(occurred);
CREATE INDEX IF NOT EXISTS ops_monitor_impact_object ON ops_monitor_incident_impact(kind,value,incident_id);
CREATE INDEX IF NOT EXISTS ops_monitor_terminal_request ON ops_monitor_observations((body->>'request_id'),occurred DESC,seq DESC)
WHERE source='gateway' AND body->>'kind' IN ('request.finished','request.cancelled');

INSERT INTO ops_monitor_request_outcomes(request_id,occurred,body)
SELECT DISTINCT ON (body->>'request_id') body->>'request_id',occurred,
 body || jsonb_build_object('outcome', CASE WHEN body->>'kind'='request.cancelled' THEN 'cancelled'
 WHEN body->>'outcome' IN ('success','failed','cancelled') THEN body->>'outcome' ELSE 'unknown' END)
FROM ops_monitor_observations
WHERE source='gateway' AND body->>'kind' IN ('request.finished','request.cancelled') AND COALESCE(body->>'request_id','')<>''
AND EXISTS(SELECT 1 FROM ops_monitor_incident_impact p WHERE p.kind='request' AND p.value=ops_monitor_observations.body->>'request_id')
ORDER BY body->>'request_id',occurred DESC,seq DESC
ON CONFLICT(request_id) DO NOTHING;
