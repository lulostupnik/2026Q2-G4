CREATE TABLE IF NOT EXISTS mock_egresos (

    referencia     TEXT PRIMARY KEY,
    cbu            TEXT        NOT NULL,
    monto_centavos BIGINT      NOT NULL,
    estado         TEXT        NOT NULL CHECK (estado IN ('acreditado', 'rechazado')),
    motivo         TEXT,

    muda          BOOLEAN     NOT NULL DEFAULT FALSE,
    creado_en     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS mock_movimientos (

    id            BIGSERIAL PRIMARY KEY,
    id_origen     TEXT        NOT NULL UNIQUE,
    referencia    TEXT,
    monto_centavos BIGINT     NOT NULL,
    ocurrido_en   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS mock_movimientos_ocurrido_idx ON mock_movimientos (id);
