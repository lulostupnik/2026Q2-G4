CREATE TABLE IF NOT EXISTS proveedor_activo (
    id             INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    url            TEXT        NOT NULL,

    actualizado_en TIMESTAMPTZ NOT NULL DEFAULT now(),
    origen         TEXT
);
