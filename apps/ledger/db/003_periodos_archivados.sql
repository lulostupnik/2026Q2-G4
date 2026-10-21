CREATE TABLE IF NOT EXISTS periodos_archivados (

    periodo       DATE PRIMARY KEY,

    clave         TEXT        NOT NULL,
    uri           TEXT        NOT NULL,

    filas         BIGINT      NOT NULL CHECK (filas >= 0),
    suma_centavos BIGINT      NOT NULL,
    archivado_en  TIMESTAMPTZ NOT NULL DEFAULT now()
);
