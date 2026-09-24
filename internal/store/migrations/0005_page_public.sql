-- Публичной может быть любая страница: флаг у страницы, адрес /public/<slug>.
-- Бывшая единственная публичная страница из настроек остаётся публичной.
ALTER TABLE pages ADD COLUMN public INTEGER NOT NULL DEFAULT 0;
UPDATE pages SET public = 1 WHERE id = (SELECT public_page_id FROM settings WHERE id = 1);
ALTER TABLE settings DROP COLUMN public_page_id;
