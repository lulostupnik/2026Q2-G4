ALTER TABLE conciliacion ADD COLUMN IF NOT EXISTS proveedor TEXT;

UPDATE conciliacion SET ultimo_cursor = NULL WHERE proveedor IS NULL;
