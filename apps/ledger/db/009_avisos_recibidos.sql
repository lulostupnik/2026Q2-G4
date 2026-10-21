CREATE TABLE IF NOT EXISTS avisos_recibidos (
    id            BIGSERIAL PRIMARY KEY,

    orden_id      BIGINT      NOT NULL,
    tipo          TEXT        NOT NULL,
    estado        TEXT        NOT NULL,
    monto_centavos BIGINT     NOT NULL DEFAULT 0,
    referencia    TEXT,

    cuerpo        JSONB       NOT NULL,

    firma_valida  BOOLEAN     NOT NULL,
    origen        TEXT,
    recibido_en   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS avisos_recibidos_recientes_idx
    ON avisos_recibidos (recibido_en DESC);
