INSERT INTO users (id, email, display_name)
VALUES
  ('11111111-1111-1111-1111-111111111111', 'jane@acme.dev', 'Jane Smith'),
  ('22222222-2222-2222-2222-222222222222', 'bob@acme.dev', 'Bob Chen'),
  ('33333333-3333-3333-3333-333333333333', 'alice@acme.dev', 'Alice Johnson')
ON CONFLICT (email) DO NOTHING;

INSERT INTO projects (id, name, key)
VALUES ('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', 'Platform', 'PROJ')
ON CONFLICT (key) DO NOTHING;

INSERT INTO statuses (project_id, name, category, sort_order)
SELECT 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', v.name, v.category, v.sort_order
FROM (VALUES
  ('To Do', 'todo', 1),
  ('In Progress', 'in_progress', 2),
  ('In Review', 'in_progress', 3),
  ('Done', 'done', 4)
) AS v(name, category, sort_order)
ON CONFLICT (project_id, name) DO NOTHING;

INSERT INTO workflow_transitions (project_id, from_status_id, to_status_id)
SELECT p.id, s1.id, s2.id
FROM projects p
JOIN statuses s1 ON s1.project_id = p.id
JOIN statuses s2 ON s2.project_id = p.id
WHERE p.id = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa'
AND (s1.name, s2.name) IN (
  ('To Do', 'In Progress'),
  ('In Progress', 'In Review'),
  ('In Review', 'Done'),
  ('In Review', 'In Progress'),
  ('In Progress', 'To Do')
)
ON CONFLICT (project_id, from_status_id, to_status_id) DO NOTHING;
