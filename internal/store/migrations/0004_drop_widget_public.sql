-- Публичной бывает страница целиком (settings.public_page_id); виджеты о публичности не знают.
ALTER TABLE widgets DROP COLUMN public;
