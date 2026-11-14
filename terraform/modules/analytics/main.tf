locals {
  database_name    = replace(var.name_prefix, "-", "_")
  history_location = "s3://${var.bucket_name}/${var.history_prefix}"

  columns = [
    { name = "id", type = "bigint", comment = "Id del asiento" },
    { name = "orden_id", type = "bigint", comment = "Orden que generó el asiento" },
    { name = "cuenta_id", type = "bigint", comment = "Cuenta afectada" },
    { name = "contraparte_id", type = "bigint", comment = "La otra cuenta del par" },
    { name = "monto_centavos", type = "bigint", comment = "Positivo entra a la cuenta, negativo sale" },
    { name = "tramo", type = "string", comment = "transferencia, reserva, liquidacion, reversa, ingreso_banco, ingreso_acreditacion o semilla" },
    { name = "creado_en", type = "timestamp", comment = "Momento del asiento, en UTC" },
  ]

  queries = {
    partida-doble = {
      description = "Cada asiento tiene su contrapartida: la suma de cada mes archivado tiene que dar 0"
      query       = <<-SQL
        SELECT periodo,
               count(*)                 AS asientos,
               count(DISTINCT orden_id) AS ordenes,
               sum(monto_centavos)      AS suma_centavos,
               sum(monto_centavos) = 0  AS cuadra
        FROM asientos
        GROUP BY periodo
        ORDER BY periodo
      SQL
    }
    volumen-mensual = {
      description = "Órdenes y volumen movido por mes y por tramo"
      query       = <<-SQL
        SELECT periodo,
               tramo,
               count(DISTINCT orden_id)                                                AS ordenes,
               sum(CASE WHEN monto_centavos > 0 THEN monto_centavos ELSE 0 END) / 100.0 AS volumen_pesos
        FROM asientos
        GROUP BY periodo, tramo
        ORDER BY periodo, volumen_pesos DESC
      SQL
    }
    cuentas-mas-activas = {
      description = "Las 20 cuentas con más órdenes en el archivo, con lo que entró y salió"
      query       = <<-SQL
        SELECT cuenta_id,
               count(DISTINCT orden_id)                                                   AS ordenes,
               sum(CASE WHEN monto_centavos > 0 THEN monto_centavos ELSE 0 END) / 100.0    AS entradas_pesos,
               -sum(CASE WHEN monto_centavos < 0 THEN monto_centavos ELSE 0 END) / 100.0   AS salidas_pesos,
               min(periodo)                                                               AS primer_periodo,
               max(periodo)                                                               AS ultimo_periodo
        FROM asientos
        GROUP BY cuenta_id
        ORDER BY ordenes DESC
        LIMIT 20
      SQL
    }
    movimientos-de-cuenta = {
      description = "Los movimientos archivados de una cuenta, en todos los meses (parámetro: cuenta_id)"
      query       = <<-SQL
        SELECT periodo,
               creado_en,
               orden_id,
               contraparte_id,
               tramo,
               monto_centavos / 100.0            AS monto_pesos
        FROM asientos
        WHERE cuenta_id = ?
        ORDER BY creado_en, id
      SQL
    }
  }
}

resource "aws_glue_catalog_database" "this" {
  name        = local.database_name
  description = "Archivo del ledger en S3, para consultar con Athena"
}

resource "aws_glue_catalog_table" "asientos" {
  name          = "asientos"
  database_name = aws_glue_catalog_database.this.name
  description   = "Asientos de los meses archivados: uno o más archivos Parquet por mes, cada uno con un rango de cuentas"
  table_type    = "EXTERNAL_TABLE"

  parameters = {
    EXTERNAL                           = "TRUE"
    classification                     = "parquet"
    "projection.enabled"               = "true"
    "projection.periodo.type"          = "date"
    "projection.periodo.format"        = "yyyy-MM"
    "projection.periodo.range"         = "${var.history_start},NOW"
    "projection.periodo.interval"      = "1"
    "projection.periodo.interval.unit" = "MONTHS"
    "storage.location.template"        = "${local.history_location}periodo=$${periodo}/"
  }

  partition_keys {
    name    = "periodo"
    type    = "string"
    comment = "Mes archivado, AAAA-MM"
  }

  storage_descriptor {
    location      = local.history_location
    input_format  = "org.apache.hadoop.hive.ql.io.parquet.MapredParquetInputFormat"
    output_format = "org.apache.hadoop.hive.ql.io.parquet.MapredParquetOutputFormat"

    ser_de_info {
      serialization_library = "org.apache.hadoop.hive.ql.io.parquet.serde.ParquetHiveSerDe"
    }

    dynamic "columns" {
      for_each = local.columns

      content {
        name    = columns.value.name
        type    = columns.value.type
        comment = columns.value.comment
      }
    }
  }
}

resource "aws_athena_workgroup" "this" {
  name          = var.name_prefix
  description   = "Consultas sobre el archivo del ledger"
  force_destroy = true

  configuration {
    enforce_workgroup_configuration    = true
    publish_cloudwatch_metrics_enabled = true
    bytes_scanned_cutoff_per_query     = var.bytes_scanned_cutoff_per_query

    engine_version {
      selected_engine_version = "Athena engine version 3"
    }

    result_configuration {
      output_location = "s3://${var.bucket_name}/${var.results_prefix}"

      encryption_configuration {
        encryption_option = "SSE_KMS"
        kms_key_arn       = var.kms_key_arn
      }
    }
  }
}

resource "aws_athena_named_query" "this" {
  for_each = local.queries

  name        = each.key
  description = each.value.description
  workgroup   = aws_athena_workgroup.this.id
  database    = aws_glue_catalog_database.this.name
  query       = each.value.query
}
