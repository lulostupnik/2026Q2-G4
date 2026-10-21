INSERT INTO cuentas (alias, tipo, permite_negativo)
VALUES ('interna:transitoria_egresos', 'interna', TRUE),
       ('interna:transitoria_ingresos', 'interna', TRUE),
       ('interna:compensacion_bancaria', 'interna', TRUE),
       ('interna:ajustes', 'interna', TRUE)
ON CONFLICT (alias) DO NOTHING;

INSERT INTO saldos (cuenta_id, saldo_centavos)
SELECT id, 0 FROM cuentas WHERE tipo = 'interna'
ON CONFLICT (cuenta_id) DO NOTHING;

INSERT INTO conciliacion (id) VALUES (1) ON CONFLICT (id) DO NOTHING;
