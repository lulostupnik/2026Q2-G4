CREATE TYPE tipo_cuenta AS ENUM ('usuario', 'comercio', 'interna');

CREATE TYPE tipo_orden AS ENUM ('transferencia', 'egreso', 'ingreso');

CREATE TYPE estado_orden AS ENUM (
    'pendiente',
    'liquidada',
    'rechazada',
    'reservada',
    'enviada',
    'acreditada',
    'devuelta',
    'en_duda',
    'atribuida',
    'no_atribuida'
);

CREATE TABLE cuentas (
    id          BIGSERIAL PRIMARY KEY,
    alias       TEXT        NOT NULL UNIQUE,
    tipo        tipo_cuenta NOT NULL,
    activa      BOOLEAN     NOT NULL DEFAULT TRUE,

    permite_negativo BOOLEAN NOT NULL DEFAULT FALSE,
    creada_en   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE saldos (
    cuenta_id      BIGINT PRIMARY KEY REFERENCES cuentas (id),
    saldo_centavos BIGINT      NOT NULL DEFAULT 0,
    actualizado_en TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE limites (
    cuenta_id                  BIGINT PRIMARY KEY REFERENCES cuentas (id),
    max_por_operacion_centavos BIGINT NOT NULL
);

CREATE TABLE ordenes (
    id              BIGSERIAL PRIMARY KEY,
    tipo            tipo_orden   NOT NULL,
    idempotency_key TEXT         NOT NULL,

    referencia      TEXT UNIQUE,
    origen_id       BIGINT REFERENCES cuentas (id),
    destino_id      BIGINT REFERENCES cuentas (id),
    cbu_destino     TEXT,
    monto_centavos  BIGINT       NOT NULL CHECK (monto_centavos > 0),
    estado          estado_orden NOT NULL,
    motivo          TEXT,

    respuesta       JSONB,
    creada_en       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    actualizada_en  TIMESTAMPTZ  NOT NULL DEFAULT now(),

    UNIQUE (tipo, idempotency_key)
);

CREATE INDEX ordenes_estado_creada_idx ON ordenes (estado, creada_en);

CREATE TABLE transiciones (
    id        BIGSERIAL PRIMARY KEY,
    orden_id  BIGINT       NOT NULL REFERENCES ordenes (id),
    desde     estado_orden,
    hasta     estado_orden NOT NULL,
    motivo    TEXT,
    creada_en TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX transiciones_orden_idx ON transiciones (orden_id, creada_en);

CREATE TABLE asientos (
    id             BIGSERIAL,
    periodo        DATE        NOT NULL,
    orden_id       BIGINT      NOT NULL,
    cuenta_id      BIGINT      NOT NULL,
    contraparte_id BIGINT      NOT NULL,

    monto_centavos BIGINT      NOT NULL,

    tramo          TEXT        NOT NULL,
    creado_en      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (periodo, id)
) PARTITION BY RANGE (periodo);

CREATE UNIQUE INDEX asientos_orden_cuenta_uq ON asientos (periodo, orden_id, cuenta_id, tramo);
CREATE INDEX asientos_cuenta_idx ON asientos (cuenta_id, creado_en DESC);

CREATE TABLE saldos_apertura (
    cuenta_id      BIGINT      NOT NULL REFERENCES cuentas (id),

    corte          DATE        NOT NULL,
    saldo_centavos BIGINT      NOT NULL,
    creado_en      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (cuenta_id, corte)
);

CREATE TABLE movimientos_banco (
    id             BIGSERIAL PRIMARY KEY,
    id_origen      TEXT        NOT NULL UNIQUE,
    referencia     TEXT,
    monto_centavos BIGINT      NOT NULL,
    ocurrido_en    TIMESTAMPTZ NOT NULL,
    procesado_en   TIMESTAMPTZ NOT NULL DEFAULT now(),
    orden_id       BIGINT REFERENCES ordenes (id),
    atribuido      BOOLEAN     NOT NULL DEFAULT FALSE
);

CREATE TABLE conciliacion (
    id              INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    ultimo_cursor   TEXT,
    ultima_consulta TIMESTAMPTZ
);

CREATE TABLE tareas (
    id                BIGSERIAL PRIMARY KEY,
    tipo              TEXT        NOT NULL,
    periodo           DATE        NOT NULL,
    proxima_ejecucion TIMESTAMPTZ NOT NULL,
    estado            TEXT        NOT NULL DEFAULT 'pendiente',
    intentos          INT         NOT NULL DEFAULT 0,
    ultimo_error      TEXT,
    UNIQUE (tipo, periodo)
);
