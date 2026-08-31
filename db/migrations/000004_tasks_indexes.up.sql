CREATE INDEX IF NOT EXISTS tasks_user_created_idx ON public.tasks (user_id, created_at DESC);
