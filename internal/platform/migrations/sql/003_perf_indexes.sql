CREATE INDEX IF NOT EXISTS idx_comments_content_tsv
ON comments USING GIN (to_tsvector('english', coalesce(content, '')));

CREATE INDEX IF NOT EXISTS idx_activity_project_event_id
ON activity_log(project_id, event_type, id DESC);

CREATE INDEX IF NOT EXISTS idx_activity_project_actor_id
ON activity_log(project_id, actor_id, id DESC);

CREATE INDEX IF NOT EXISTS idx_activity_project_issue_id
ON activity_log(project_id, issue_id, id DESC);
