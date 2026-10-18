-- role and members are no longer written; workspace_members is the source of truth.
ALTER TABLE workspaces ALTER COLUMN role SET DEFAULT 'owner';
