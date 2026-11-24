-- Why the run was reviewed: a queue reason (incident, truncated, refused,
-- tool_error, sample) or the run's own silent-failure flag; '' for an
-- unflagged run reviewed outside the queue. Accuracy alerts compare unflagged
-- reviews with unflagged reviews.
ALTER TABLE run_reviews ADD COLUMN IF NOT EXISTS reason TEXT NOT NULL DEFAULT '';
