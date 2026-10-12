-- Приватная группа публичной страницы видна только после входа.
ALTER TABLE page_groups ADD COLUMN private INTEGER NOT NULL DEFAULT 0;
