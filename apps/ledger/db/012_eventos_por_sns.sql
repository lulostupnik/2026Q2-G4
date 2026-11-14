DROP TABLE IF EXISTS entregas_webhook;

ALTER TABLE avisos_recibidos ADD COLUMN evento_id TEXT UNIQUE;
