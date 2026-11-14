# analytics

Consultas SQL con Athena sobre el archivo del ledger, sin tocar la base transaccional. El
archivador deja cada mes en `transaction-history/periodo=AAAA-MM/`, en uno o más archivos Parquet
(`parte-000.parquet`, `parte-001.parquet`, …), cada uno con un rango de cuentas y ordenado por
cuenta. El módulo lo cataloga en Glue como la tabla `asientos`, particionada por `periodo`.

- **Parquet.** Athena lee sólo las columnas que usa la consulta, reparte los archivos y sus
  bloques entre varios workers y, con `WHERE cuenta_id = …`, saltea los bloques que no contienen
  la cuenta (cada bloque guarda el mínimo y el máximo de cada columna). Escanea mucho menos que
  sobre JSON, y Athena cobra por byte escaneado.
- **Particiones por proyección.** La tabla no guarda particiones en el catálogo: Athena las
  calcula desde `history_start` hasta el mes actual, a partir de las carpetas `periodo=AAAA-MM/`.
  Un mes recién archivado se puede consultar enseguida, sin `MSCK REPAIR TABLE` ni crawler: el
  esquema y las carpetas los define el propio archivador, así que no hay nada que descubrir.
- **Workgroup con la configuración forzada.** Los resultados van a `athena-results/` del mismo
  bucket, cifrados con la clave del proyecto, y cada consulta tiene un tope de bytes escaneados.
  `storage` los expira a los 7 días; como el bucket está versionado, pasan a ser una versión
  anterior y se borran 30 días después.
- **Consultas guardadas** en el workgroup: `partida-doble`, `volumen-mensual`,
  `cuentas-mas-activas` y `movimientos-de-cuenta` (con `cuenta_id` como parámetro).

`creado_en` es un `timestamp` en UTC. `periodo` no es una columna del archivo: es la partición, en
formato `AAAA-MM`.

<!-- BEGIN_TF_DOCS -->
<!-- END_TF_DOCS -->
