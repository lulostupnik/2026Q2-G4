CREATE TABLE IF NOT EXISTS entregas_webhook (
    orden_id     BIGINT PRIMARY KEY REFERENCES ordenes (id),
    estado       TEXT        NOT NULL CHECK (estado IN ('pendiente', 'entregada', 'fallida')),
    intentos     INT         NOT NULL DEFAULT 0,
    ultimo_error TEXT,
    creada_en    TIMESTAMPTZ NOT NULL DEFAULT now(),
    actualizada_en TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS solicitudes_exportacion (
    id            BIGSERIAL PRIMARY KEY,

    cuenta_id     BIGINT REFERENCES cuentas (id),

    desde         DATE        NOT NULL,
    hasta         DATE        NOT NULL,
    estado        TEXT        NOT NULL CHECK (estado IN ('pendiente', 'lista', 'fallida')),

    clave         TEXT,
    uri           TEXT,
    filas         BIGINT,

    periodos_base    INT,
    periodos_archivo INT,
    motivo        TEXT,
    solicitada_en TIMESTAMPTZ NOT NULL DEFAULT now(),
    resuelta_en   TIMESTAMPTZ,
    CHECK (hasta >= desde)
);

CREATE INDEX IF NOT EXISTS solicitudes_exportacion_cuenta_idx
    ON solicitudes_exportacion (cuenta_id, solicitada_en DESC);
