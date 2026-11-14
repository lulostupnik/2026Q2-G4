CREATE TABLE IF NOT EXISTS archivo_partes (
    periodo       DATE        NOT NULL REFERENCES periodos_archivados (periodo) ON DELETE CASCADE,
    parte         INT         NOT NULL CHECK (parte >= 0),
    clave         TEXT        NOT NULL,
    cuenta_min    BIGINT      NOT NULL,
    cuenta_max    BIGINT      NOT NULL,
    filas         BIGINT      NOT NULL CHECK (filas > 0),
    suma_centavos BIGINT      NOT NULL,
    bytes         BIGINT      NOT NULL CHECK (bytes > 0),
    PRIMARY KEY (periodo, parte),
    CHECK (cuenta_max >= cuenta_min)
);

CREATE INDEX IF NOT EXISTS archivo_partes_cuentas_idx ON archivo_partes (periodo, cuenta_min, cuenta_max);

ALTER TABLE solicitudes_exportacion ADD COLUMN IF NOT EXISTS tomada_en TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS solicitudes_exportacion_pendientes_idx
    ON solicitudes_exportacion (id) WHERE estado = 'pendiente';
