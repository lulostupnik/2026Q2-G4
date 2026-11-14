# Arquitectura — Núcleo Transaccional Elástico

Este documento describe la arquitectura que despliega este repositorio y los trade-offs de esta
iteración. El código Terraform es la fuente de verdad; acá se resume la intención. Cómo
desplegarlo está en el [README](README.md).

## 1. Contexto

El sistema es el núcleo transaccional de un PSP de una sola empresa. Cada usuario tiene una
cuenta virtual con la que transfiere a otras cuentas del PSP, ingresa dinero desde su banco y
lo retira hacia afuera a través de un proveedor de infraestructura financiera. Cada operación
se liquida dentro del mismo request, de forma idempotente, contra un ledger de partida doble.
Lo único diferido es lo que ningún usuario espera: el archivado de los meses viejos a S3 y
el webhook de cada orden terminada, que sale por SNS y SQS después de responder.

Restricciones que guían el diseño:

- **AWS Academy**: no se pueden crear usuarios ni roles de IAM. Las instancias usan
  `LabInstanceProfile` y las tareas de ECS y las Lambdas usan `LabRole`. Sólo está habilitada
  `us-east-1`.
- **Reproducible desde el pipeline**: todo vive en código y se despliega y destruye desde
  GitHub Actions, en cualquier cuenta, sin editar nada. Los nombres que tienen que ser únicos
  llevan el ID de la cuenta.
- **Costo acotado**: un solo NAT, la API en Graviton (más barato que x86 a igual tamaño) y el
  archivador en una `t3.medium`, apagada fuera de su ventana.

## 2. Red

VPC `10.0.0.0/16` con el módulo `terraform-aws-modules/vpc/aws`, dos AZ y cuatro capas:

| Capa | CIDR (AZ a, AZ b) | Salida | Qué corre |
|---|---|---|---|
| `public` | `10.0.0.0/24`, `10.0.1.0/24` | IGW | NLB, NAT |
| `app` | `10.0.10.0/24`, `10.0.11.0/24` | NAT, gateway endpoint de S3 | Instancias y tareas de la API, ENIs de los interface endpoints |
| `archive` | `10.0.12.0/24`, `10.0.13.0/24` | Sólo el gateway endpoint de S3 | Instancias y tareas del archivador |
| `data` | `10.0.20.0/24`, `10.0.21.0/24` | Ninguna | RDS (primaria y standby) |

El aislamiento entre capas lo imponen los security groups, que se referencian entre sí:

| SG | Entra | Sale |
|---|---|---|
| `nlb` | 80 desde Internet | La API (puerto de la app) |
| `app` | Puerto de la app desde `nlb` | `data` (5432), S3 (443), `endpoints` (443: SNS), Internet por el NAT (443 y 80: proveedor) |
| `archive` | Nada | `data` (5432), S3 (443) |
| `data` | 5432 desde `app` y `archive` | Nada |
| `host` | Nada | `endpoints` (443), S3 (443) |
| `endpoints` | 443 desde `host` y `app` | Nada |

Las instancias (`host`) no aceptan ninguna conexión entrante: no hay SSH.

Las NACL quedan en su regla permisiva por defecto, a propósito. El aislamiento ya lo dan dos
controles que fallan de forma distinta: el ruteo (`data` y `archive` no tienen ruta a
Internet, así que no existe el camino) y los security groups (que impiden la conexión aunque el
camino exista). Una NACL no tiene estado: repetir esas reglas obligaría a abrir a mano los
puertos efímeros de vuelta, y sumaría superficie de error sin agregar aislamiento. Se usaría
para negar un origen puntual, que es lo único que un security group no puede expresar.

### Entrada: por qué un NLB

Fue la corrección de la segunda entrega (ver
[Correcciones](#11-correcciones-de-entregas-anteriores)): por la naturaleza de la aplicación,
el balanceador correcto es uno de red y no uno de aplicación. Un PSP es un núcleo transaccional,
no un sitio web, y lo que necesita de la entrada es lo que da la capa 4:

- **Latencia mínima en cada pago.** El caso que fija la latencia es un pago con una persona
  esperando en la caja. El NLB reenvía la conexión TCP tal cual, sin abrir el HTTP ni armar una
  segunda conexión hacia la tarea: agrega microsegundos, donde un ALB agrega milisegundos por
  request.
- **Picos bruscos sin calentamiento.** El volumen se multiplica en pocas horas (el cobro de
  haberes). Un NLB atiende millones de requests por segundo desde el primer minuto; un ALB
  escala de a poco y, ante un salto brusco, puede responder 503 mientras crece, salvo que se le
  pida a AWS que lo precaliente.
- **Conexiones largas.** Comercios, terminales y proveedores mantienen conexiones abiertas y
  reutilizadas; el NLB las sostiene sin cortarlas por request.
- **Protocolos que no son HTTP.** La red de pagos (adquirentes, terminales POS) habla ISO 8583
  sobre TCP. Un ALB sólo entiende HTTP y gRPC; un NLB deja recibir ese tráfico el día que el
  núcleo se conecte directo a la red, sin cambiar la entrada.
- **TLS de punta a punta.** Con un NLB el TLS puede terminar en la aplicación (*pass-through*),
  así los datos del pago no se descifran en un intermediario, que es lo que pide PCI DSS para
  la transmisión de datos de tarjeta. Hoy el listener es TCP:80 porque el laboratorio no tiene
  dominio para emitir un certificado.
- **IP fija por zona.** Bancos y proveedores financieros autorizan por lista blanca de IP. Un
  NLB puede tener una Elastic IP por zona; las IP de un ALB cambian. Hoy la salida hacia el
  proveedor ya tiene IP fija (la del NAT, `nat_public_ip`).
- **Nada de lo que aporta un ALB hace falta.** Detrás hay un único servicio, así que no hay
  ruteo por path ni por host; la autenticación la hace la aplicación con el token de Cognito, y
  no hay contenido que reescribir o redirigir.

Cómo está armado: escucha TCP:80 en las subredes públicas de las dos AZ, con balanceo entre
zonas, y reenvía a la IP de cada tarea de la API (targets `ip`, porque con red `awsvpc` cada
tarea tiene la suya); ECS registra y saca las tareas solo. Tiene su propio security group, así
que la regla de `app` dice "desde el NLB" y no "desde un rango". El health check sí es HTTP:
pide `/health`, que responde 200 mientras el proceso esté vivo e informa en el cuerpo si llega a
la base, para que una caída de la base no le haga sacar al NLB todas las tareas a la vez.

Lo que se resigna: la métrica de requests por target (`ALBRequestCountPerTarget`) sólo existe en
un ALB, así que la API escala por CPU; y AWS WAF no se puede poner delante de un NLB (va en ALB,
CloudFront o API Gateway), así que la validación de cada pedido queda en la aplicación.

## 3. Cómputo

La API y el archivador corren en **ECS con launch type EC2**, desde la misma imagen (cada
servicio elige `/api` o `/archivador`), construida para amd64 y arm64. Cada servicio tiene su
propio ASG, en sus propias subredes:

| | API | Archivador |
|---|---|---|
| Subredes (instancias y tareas) | `app`, con NAT | `archive`, sin ruta a Internet |
| Tareas | 2 a 5, escalan por CPU (60 %) | 0, y 1 durante la ventana de archivado |
| Tipo de instancia | `c7g.large` (Graviton), dos tareas por instancia | `t3.medium`, una tarea |
| Instancias | Las que hagan falta para esas tareas, con lugar libre | 0 fuera de la ventana |
| Tráfico | NLB → IP de cada tarea | Ninguno: sólo sale a la base y a S3 |

Hay **dos niveles de escalado**: el servicio decide cuántas tareas quiere (por CPU o por
horario) y el capacity provider agrega o apaga instancias para alojarlas. Con red `awsvpc`
cada tarea tiene su propia ENI y su security group, y la instancia se queda una para ella: una
`c7g.large` (3 ENIs) aloja dos tareas, y la `t3.medium` del archivador, una sola: la tarea pide
3 GB de los 4 que tiene.

El capacity provider de la API apunta a tener ocupado el 80 % de las instancias
(`target_capacity`), para dejar lugar de reserva: la primera tarea nueva de un scale-out o de un
deploy suele encontrar dónde arrancar sin esperar, y el capacity provider lanza la siguiente
instancia en segundo plano. No es una garantía: si llegan más tareas de las que entran en esa
reserva, las que sobran esperan a que la instancia nueva se registre en el cluster. El del archivador queda en 100 %: no tiene picos, y fuera de su
ventana baja a cero instancias.

**Por qué EC2 y no Fargate.** La API es una carga base constante: un PSP no baja a cero y
siempre hay dos tareas corriendo. Para eso EC2 sale más barato por vCPU que Fargate (y en una
cuenta propia se le suman Savings Plans), deja elegir el hardware y permite que una tarea use
en un pico la CPU que el host tiene libre, que en Fargate es un límite fijo. Las contras son el
escalado en dos niveles, más lento, que compensa el lugar libre de arriba, y los parches de las
instancias, que quedan a cargo nuestro: el launch template toma en cada apply la AMI
recomendada, pero sólo la usan las instancias nuevas.

**Por qué esos tipos de instancia.** AWS Academy permite hasta el tamaño `large`. La API
corre en `c7g.large`: no es burstable, así que su CPU no depende de créditos; Graviton cuesta
menos que x86 a igual tamaño, y sus 2 vCPU coinciden con las dos tareas de 1 vCPU que le
permiten sus ENIs. El archivador es un batch de una tarea, unas horas por día, que nadie
espera, así que va en una instancia burstable: su CPU en ráfaga cubre esa ventana. Es una
`t3.medium` por la memoria: escribe varias partes de Parquet en paralelo y cada una arma un
bloque en memoria antes de subirlo (ver [Datos](#4-datos)). En una cuenta
propia iría en **Spot**, que AWS Academy no habilita. El archivado se puede retomar: cada
período es una fila de `tareas` que se toma con `FOR UPDATE SKIP LOCKED` y, si la instancia se
interrumpe, se reintenta, así que una interrupción sólo lo atrasa. Se configuraría con un ASG
con `mixed_instances_policy` sin On-Demand, varios tipos chicos para tener más pools de Spot y
`ECS_ENABLE_SPOT_INSTANCE_DRAINING=true`, para que ECS saque la tarea con el aviso de dos
minutos.

**Agente de ECS.** El `user_data` de las instancias, además del cluster, configura
`ECS_IMAGE_PULL_BEHAVIOR=prefer-cached`, que reutiliza la imagen si ya está en el host (es
seguro porque los tags de ECR son inmutables), y `ECS_AWSVPC_BLOCK_IMDS=true`, que impide que
las tareas lean las credenciales de la instancia del servicio de metadatos: usan las suyas, las
del task role. En el laboratorio las dos son `LabRole`, pero en una cuenta propia el rol de la
instancia tiene permisos que la tarea no necesita.

El agente de ECS de cada instancia necesita hablar con ECS, ECR y CloudWatch Logs. Como las
subredes de `archive` no tienen salida, lo hace por **interface endpoints** (`ecs`,
`ecs-agent`, `ecs-telemetry`, `ecr.api`, `ecr.dkr`, `logs`, `secretsmanager`, `sns`); las capas de la
imagen vienen de S3 por el gateway endpoint. Las ENIs de los endpoints están en las subredes de `app`, pero se
llega a ellas desde cualquier subred de la VPC: lo que limita quién entra es su security group,
que acepta a los hosts de ECS y, para publicar en SNS, a las tareas de la API. Las instancias de
la API usan los mismos endpoints, así que el NAT sólo lo usan las tareas de la API (proveedor y
claves de Cognito).

Todo el tráfico de la VPC hacia S3 de la región pasa por el gateway endpoint, también el de las
subredes de `app`. Su política sólo deja llegar al bucket del proyecto y al de las capas de ECR:
el archivador y las instancias, que no tienen otra salida, no pueden copiar datos a un bucket de
otra cuenta. El security group no alcanza para eso, porque la prefix list de S3 incluye todos
los buckets. Las tareas de la API sí salen a Internet por el NAT, así que para ellas la política
sólo cubre S3 de esta región. El bucket, por su lado, rechaza cualquier pedido que no venga por
HTTPS.

Los interface endpoints, en cambio, sólo aceptan credenciales de la propia cuenta. Lo que pasa
por ellos lo firma el rol de las instancias, así que el agente, la descarga de la imagen y los
logs no cambian, pero nadie puede usarlos con credenciales de otra cuenta para mandar datos
afuera. En S3 esa condición no sirve: las capas de ECR se bajan con URLs que firma AWS, no la
cuenta, y por eso ahí la política lista los buckets.

**Infraestructura inmutable.** Nada se modifica en caliente: una versión nueva del backend es
una imagen nueva en ECR (los tags son inmutables, así que un tag siempre es la misma imagen) y
una revisión nueva de la task definition, y ECS reemplaza las tareas en lugar de actualizarlas.
No hay SSH ni configuración manual en las instancias. La excepción es la AMI: el launch template
toma la recomendada en cada apply, pero sólo la usan las instancias nuevas. No se usa
`prevent_destroy` porque el workflow Destroy tiene que poder borrar todo, ni hace falta
`create_before_destroy`: los reemplazos de tareas los hace ECS, sin cortar el servicio.

Un deploy con cambios en el backend sube una imagen nueva y crea una revisión de cada task
definition. ECS reemplaza las tareas de a tandas: nunca baja de la mitad de las deseadas ni pasa
del doble. El ASG admite una instancia más que el máximo de tareas, y eso alcanza para ese doble
en los dos servicios: 6 `c7g.large` alojan 12 tareas de la API y 2 `t3.medium`, dos del
archivador. Por eso la API llega a 5 tareas y no más: su ASG queda en 6, el máximo que AWS
Academy deja poner en un ASG. Una
tarea nueva de la API recibe tráfico recién cuando pasa el health check del NLB, y si las
nuevas no arrancan, el circuit breaker vuelve solo a la revisión anterior. Eso vale mientras la revisión nueva no haya
aplicado una migración: un binario no arranca contra una base con migraciones que no conoce,
así que las tareas viejas que siguen corriendo no se caen, pero las que ECS intente levantar con
la revisión anterior fallan, y la salida es otro deploy hacia adelante. El tope de conexiones a
la base de cada tarea se calcula como si los dos servicios llegaran a la vez al doble de su
máximo, así que un deploy no agota las conexiones de RDS.

El cluster tiene **Container Insights**: CPU, memoria y red por servicio y por tarea en
CloudWatch, sin instalar nada en las instancias.

**Escaneo de la imagen.** El repositorio de ECR tiene `scan_on_push`: cada imagen que sube
`make imagen` se compara contra la base de vulnerabilidades conocidas (CVE) y el resultado
queda en la consola de ECR (`aws ecr describe-image-scan-findings`). Es el escaneo básico,
sin costo, y alcanza para esta imagen: la base es `distroless/static`, sin shell ni gestor de
paquetes, así que lo que podría aparecer está en las pocas librerías del sistema. El escaneo
avanzado (Amazon Inspector) revisa además las dependencias de Go, pero se cobra por imagen.
No bloquea el deploy: informa, y como los tags son inmutables cada versión queda escaneada
una sola vez, la vez que se sube.

## 4. Datos

RDS PostgreSQL 18 Multi-AZ (`db.t3.micro`) en las subredes de `data`, sin acceso público,
cifrada con la clave del proyecto y con 7 días de backups automáticos. La standby está en la
otra AZ, con réplica sincrónica y failover automático sin cambiar el endpoint. No es una
réplica de lectura: no atiende tráfico, y nunca se autoriza un pago contra datos con retraso.

**Por qué relacional y no DynamoDB.** El invariante del ledger es transaccional: una
transferencia son dos asientos y dos saldos que se escriben juntos o no se escribe ninguno, con
integridad referencial entre cuentas, órdenes y asientos. Validar fondos necesita un bloqueo de
fila: la liquidación hace `SELECT … FOR UPDATE` sobre `saldos` mientras valida y escribe, y toma
los bloqueos en orden de id de cuenta, para que dos transferencias cruzadas no se traben entre
sí. La idempotencia es una restricción `UNIQUE (tipo, idempotency_key)` en `ordenes`: un
reintento con la misma clave devuelve la orden que ya existe en lugar de liquidar dos veces. Y
los extractos y el cierre son agregaciones por rango de fechas sobre entidades relacionadas. En
DynamoDB todo eso habría que construirlo en la aplicación, con escrituras condicionales y
reintentos.

**Modelo del saldo**, en tres partes:

- `asientos`: la fuente de verdad, de sólo anexado y particionada por mes
  (`PARTITION BY RANGE (periodo)`).
- `saldos`: el saldo vigente de cada cuenta, que se actualiza en la misma transacción que los
  asientos. Es lo que se lee y se bloquea, así que validar fondos cuesta lo mismo con diez
  asientos que con diez millones.
- `saldos_apertura`: el saldo de cada cuenta al corte de cada archivado, para seguir derivando
  el saldo cuando los meses viejos ya no están en la base. La API expone el saldo vigente y el
  derivado y muestra si coinciden.

**Archivado.** El archivador toma un período de la tabla `tareas` (`FOR UPDATE SKIP LOCKED`),
hace `DETACH PARTITION` de ese mes, lo sube a S3 y borra la partición. No hay un `DELETE` masivo
que genere WAL ni compita por bloqueos con la liquidación.

El mes se guarda en **Parquet**, partido por rangos de cuentas, bajo un prefijo estilo Hive que
se consulta con Athena:

```
transaction-history/periodo=2025-06/parte-000.parquet   cuentas 1 – 41.200
transaction-history/periodo=2025-06/parte-001.parquet   cuentas 41.201 – 83.950
```

- **Partes.** El archivador cuenta las filas de cada cuenta y las agrupa hasta
  `archive_rows_per_part` (5 millones por defecto). Una cuenta nunca queda en dos partes. Las
  partes se escriben en paralelo, cada una con su conexión, y se suben por multipart a medida
  que se escriben: la memoria queda acotada a un bloque por parte y no depende del tamaño del
  mes.
- **Bloques.** Cada parte se ordena por cuenta y fecha y se divide en bloques (*row groups*)
  de `archive_rows_per_row_group` filas. Parquet guarda el mínimo y el máximo de cada columna
  por bloque, así que cada bloque cubre un rango chico de cuentas.
- **Verificación.** Antes de borrar la partición, cada parte se compara con la base (filas y
  suma de montos de su rango de cuentas, sacadas del mismo recuento que arma las partes) y se
  relee su pie desde S3 para confirmar que quedó entera. Como el recuento cubre toda la
  partición, si coinciden todas las partes coincide el total. Si algo no coincide, la partición
  queda desenganchada pero intacta y el archivado se reintenta.
- **Índice.** `archivo_partes` guarda, por mes, cada parte con su rango de cuentas, sus filas
  y su tamaño, en la misma transacción que marca el mes como archivado.

**Extracto de un mes archivado.** La API busca en `archivo_partes` la parte que tiene la cuenta,
lee el pie del archivo con un GET por rango (unos KB, con el mínimo y el máximo de cuentas de
cada bloque y el índice de páginas) y elige el bloque de la cuenta. Dentro del bloque, el índice
dice qué páginas de `cuenta_id` tienen la cuenta y dónde está cada página de las demás columnas,
así que la API baja sólo esas páginas, con unos pocos GET por rango en paralelo. Los archivos no
cambian, así que el pie queda en memoria: un extracto trae de S3 decenas o cientos de KB, sin
importar cuánto pese el mes ni el bloque. Las respuestas son las mismas que cuando el mes está
en la base.

**Exportaciones.** `POST /exportaciones` registra la solicitud y responde enseguida; un proceso
de cada tarea de la API la toma (`FOR UPDATE SKIP LOCKED`, con un plazo de 15 minutos por si la
tarea muere a mitad de camino), recorre los meses de la base y del archivo y sube el resultado a
`exports/` en streaming. Cuando queda lista, `GET /exportaciones/{id}/descarga` firma una URL
prefirmada de S3 que vence a los 15 minutos, y el archivo se baja directo de S3, sin pasar por
la API. Lo que dura días es el archivo (`export_retention_days`), no el link: cada descarga firma
uno nuevo. Un link largo no conviene porque cualquiera que lo tenga descarga el extracto, y
además una URL prefirmada deja de valer cuando vencen las credenciales temporales del rol que la
firmó.

### Análisis del archivo con Athena

Un mes archivado sale de RDS, y lo único que lo leía era el extracto de una cuenta en un mes. Las
preguntas que cruzan meses y cuentas (si cada mes cuadra, cuánto se movió por tramo, la historia
completa de una cuenta) se responden con SQL sobre el archivo, en Athena
([`modules/analytics`](terraform/modules/analytics)), sin cargar la base que liquida los pagos.

- **Catálogo.** Una base de Glue con la tabla externa `asientos` sobre `transaction-history/`,
  en Parquet. Athena lee sólo las columnas de cada consulta, reparte las partes y sus bloques
  entre varios workers y, con un filtro por `cuenta_id`, saltea los bloques que no la tienen.
  `creado_en` es un `timestamp` en UTC, y `periodo` no es columna del archivo: es la partición.
- **Partition projection.** La tabla no registra particiones: Athena las calcula desde
  `history_start` (enero de 2024) hasta el mes actual y arma la ruta de cada una con
  `storage.location.template`. Un mes recién archivado se ve en la próxima consulta, sin
  `MSCK REPAIR TABLE` y sin crawler. Un crawler sirve para descubrir el esquema y las carpetas de
  datos que no se controlan; acá los define el propio archivador, así que no hay nada que
  descubrir, y un crawler sólo sumaría demora (el mes aparece recién cuando vuelve a correr),
  costo por corrida y el riesgo de inferir mal un tipo. Un filtro por `periodo` limita lo que se
  lee a esos meses.
- **Workgroup `ledger`.** Fuerza su configuración sobre la del cliente: los resultados van a
  `athena-results/` del bucket del proyecto, cifrados con SSE-KMS y la clave del proyecto. El
  lifecycle los expira a los 7 días, pero como el bucket está versionado expirar no los borra:
  quedan como versión anterior y los elimina la regla de versiones reemplazadas 30 días después,
  así que duran unos 37 días. Pone un tope de 1 GiB escaneado por consulta, porque Athena
  cobra por byte leído, y publica sus métricas en CloudWatch.
- **Consultas guardadas.** `partida-doble` (la suma de cada mes tiene que dar 0, porque cada
  asiento tiene su contrapartida), `volumen-mensual`, `cuentas-mas-activas` y
  `movimientos-de-cuenta`, con `cuenta_id` como parámetro.
- **Clase de almacenamiento.** El archivo pasa a STANDARD_IA a los 30 días y a GLACIER Instant
  Retrieval al año. Antes iba a GLACIER (Flexible Retrieval), que Athena saltea sin error: una
  consulta de más de un año habría dado totales incompletos. Tampoco la API podía leer ese objeto
  para el extracto sin restaurarlo antes. Instant Retrieval se lee como cualquier objeto y sigue
  siendo la clase más barata para datos que casi no se consultan.
- **Red y permisos.** Athena corre fuera de la VPC y lee S3 con las credenciales de quien
  consulta (el usuario del laboratorio o `LabRole`), que la política de la clave KMS ya habilita
  para descifrar. No pasa por el gateway endpoint ni hace falta un endpoint nuevo. La API no
  consulta Athena: el análisis es para operación y auditoría, no para el camino de un pago.

**Conexiones.** Cada tarea abre como máximo `pool_max_conns` conexiones, que va en la
`DB_URL`. Se calcula repartiendo `db_max_connections` (60 por defecto, por debajo de las ~80 de
una `db.t3.micro`) como si los dos servicios estuvieran a la vez en el doble de su máximo de
tareas, que es el pico de un deploy.

## 5. Secretos y cifrado

Las claves que usan los binarios (la `DB_URL` con la password de la base, la de admin, la del
mock, la del proveedor y la del webhook) no van en la task definition, donde cualquiera con
acceso a la consola o a `ecs:DescribeTaskDefinition` las vería. Terraform las genera
(`random_password`) y las guarda en **Secrets Manager**, un secreto por servicio con sólo lo
que ese servicio usa: `ledger/api` tiene las cinco, `ledger/archive` sólo la `DB_URL`.

La task definition guarda la referencia, no el valor: cada variable es un `valueFrom` con
`<arn del secreto>:<clave>::<version-id>`. El agente de ECS lee el secreto con el rol de
ejecución antes de arrancar el contenedor y se lo pasa como variable de entorno, así que el
código no cambia. El `version-id` hace que un valor nuevo genere una revisión nueva de la task
definition, y ECS reemplaza las tareas; sin él seguirían corriendo con el valor viejo. El
agente llega a Secrets Manager por su interface endpoint, porque las subredes de `archive` no
salen a Internet.

El client secret de Cognito está en un tercer secreto, `ledger/callback`. La Lambda del
callback sólo recibe su ARN y lo lee con el SDK una vez por instancia; si la lectura falla no
queda guardado y el próximo login reintenta. La Lambda no corre en la VPC, así que no necesita
endpoint. La Lambda del webhook hace lo mismo con `ledger/webhook`, que tiene sólo el secreto
HMAC.

Así se sigue lo que pide la clase de seguridad, no dejar secretos en el código, el repositorio
ni las variables de entorno de la configuración: el valor no está en el repo ni en la task
definition, sólo en Secrets Manager. El contenedor sí lo recibe como variable de entorno al
arrancar, porque es lo que lee el binario; queda en la memoria del proceso, no en ningún lugar
visible desde la consola.

**Sin rotación automática.** Secrets Manager puede rotar solo, pero eso necesita una Lambda de
rotación con su rol, y AWS Academy no deja crear roles. Rotar hoy es cambiar el valor en
Terraform: el `version-id` de la referencia cambia y ECS reemplaza las tareas. Una rotación
hecha a mano fuera de Terraform no llegaría a las tareas, porque siguen apuntando a la versión
fija.

Los secretos se crean con `recovery_window_in_days = 0`: Secrets Manager reserva por defecto
el nombre 30 días después de borrarlo, y el siguiente Deploy fallaría por nombre duplicado.

**Clave KMS propia.** Secrets Manager, S3, ECR y CloudWatch Logs cifran siempre en reposo, aun
sin indicar nada, con claves que administra AWS; RDS y EBS cifran sólo si se pide, y también
pueden usar una clave de AWS (`aws/rds`, `aws/ebs`). Lo que esas claves no permiten es
*gestionarlas*: su política no se puede modificar, no se pueden deshabilitar y
la rotación no la decide uno. Para un PSP eso importa: PCI DSS (requisitos 3.6 y 3.7) pide
restringir el acceso a las claves criptográficas a la menor cantidad posible de custodios,
rotarlas y poder retirar una clave comprometida. Por eso hay una clave propia, `alias/ledger`
([`kms.tf`](terraform/organization/kms.tf)):

- **Qué cifra:** los secretos de Secrets Manager, el bucket del archivo (SSE-KMS con *bucket
  key*, una clave de datos por bucket en lugar de una llamada a KMS por objeto), los resultados
  de Athena, el
  almacenamiento y los backups de RDS, las imágenes de ECR, los topics de SNS, las colas de
  SQS y los logs de ECS, de las Lambdas y de API Gateway. Los discos raíz de las instancias de ECS se cifran con la clave de AWS para
  EBS (`aws/ebs`) y no con la del proyecto: para que Auto Scaling lance instancias con discos
  cifrados con una clave propia, su rol de servicio tendría que figurar en la política de la
  clave.
- **Quién la usa:** la política nombra a la cuenta (sin ese permiso la clave quedaría
  inadministrable), al `LabRole`, sólo para las operaciones de cifrado (nombrado explícitamente
  para no depender de lo que permita su política de IAM en el laboratorio), a CloudWatch Logs,
  limitado a los log groups de la cuenta, y a SNS y CloudWatch, sólo para cifrar y descifrar:
  SNS escribe en la cola cifrada y CloudWatch publica las alarmas en el topic cifrado. El permiso de la cuenta delega en
  IAM, y la política de IAM del `LabRole` es amplia: en la práctica también puede administrar y
  borrar la clave (ver [Trade-offs](#10-trade-offs)).
- **Rotación:** automática, anual. KMS conserva las versiones anteriores, así que lo cifrado
  antes se sigue pudiendo leer.
- **Retiro:** deshabilitar la clave corta de una vez el acceso a todos esos datos. Al
  destruir, KMS la programa para borrarse en 7 días (el mínimo); el próximo Deploy crea otra.

El bucket del frontend queda con AES256: lo leen usuarios anónimos por el endpoint de website,
y un anónimo no puede tener permiso sobre una clave de KMS.

## 6. Login con Cognito

Cognito sólo acepta callbacks HTTPS y el frontend se sirve por HTTP desde S3, así que el
callback pasa por API Gateway y una Lambda:

1. El frontend lee `config.json` y redirige a `login_url` agregando `&state=<random>`.
2. La Hosted UI de Cognito vuelve a `https://<api-gateway>/callback?code=…&state=…`.
3. La Lambda canjea el `code` por tokens (con el client secret, que lee de Secrets Manager,
   `ledger/callback`) y redirige
   a `<frontend>/#access_token=…&id_token=…&expires_in=…&state=…`.
4. El frontend valida el `state`, guarda los tokens y limpia la URL.
5. El frontend llama a la API con `Authorization: Bearer <access_token>`. La API valida la
   firma contra `<issuer>/.well-known/jwks.json` y comprueba `client_id` y `token_use=access`.
   Por CORS, sólo acepta pedidos del navegador que vengan de `website_url` (`CORS_ORIGENES`).
6. La primera vez, `GET /sesiones/actual` devuelve `cuenta_id: null` y el frontend crea la cuenta
   con `POST /cuentas`. La cuenta queda ligada al `sub` del usuario: cada usuario tiene una sola,
   y es la única que puede leer y operar (si no, 403).

Si el token vence (según `expires_in`) o la API responde 401, el frontend borra los tokens y
vuelve a la pantalla de ingreso. Para cerrar sesión, borra los tokens y redirige a
`logout_url`. Cognito vuelve por `/logout` de API Gateway, porque tampoco acepta logout URLs
HTTP.

`config.json` lo genera Terraform (`api_url`, `login_url`, `logout_url`), así que no va en
`apps/web/`. Los datos para la API (`issuer`, `client_id`) y el `user_pool_id` para crear
usuarios por CLI están en `terraform output cognito`.

## 7. Eventos y notificaciones

Cuando una orden llega a un estado terminal, la API publica un evento en el topic SNS
`ledger-events` ([`modules/events`](terraform/modules/events)) y responde sin esperar. El topic
desacopla a la API de quién escucha: hoy lo consume el webhook, y sumar otro consumidor es
sumar una suscripción, sin tocar la API.

| Consumidor | Camino | Si falla |
|---|---|---|
| Webhook | SNS → SQS `ledger-webhook` → Lambda ([`apps/webhook`](apps/webhook)) → POST firmado | SQS lo intenta hasta 5 veces; después va a la DLQ, que dispara una alarma |

- **Por qué lleva cola.** El webhook es un contrato con otro sistema, cuyo endpoint puede estar
  caído un rato: la cola guarda el aviso y lo reintenta, y lo que agota los reintentos queda en
  la DLQ para revisarlo.
- **Qué resuelve.** Antes el webhook salía de una goroutine dentro de la tarea de la API: si ECS
  la apagaba (scale-in o deploy), los avisos en curso quedaban colgados. Ahora la entrega no
  depende de la tarea, y cambiar `webhook_url` actualiza la Lambda sin reemplazar tareas.
- **Contrato del aviso.** El mismo de antes (JSON firmado con HMAC en `X-Ledger-Signature`),
  más `evento_id = <orden_id>:<estado>`. SQS entrega al menos una vez, así que el receptor
  descarta los `evento_id` repetidos, sólo si la firma es válida: si no, una firma falsa podría
  ocupar el id de un aviso real. El mensaje lleva `tipo` y `estado` como atributos, para que
  una suscripción nueva pueda filtrar sin leer el cuerpo.
- **Alertas.** Un topic aparte, `ledger-alerts`, manda por email la alarma de la DLQ a las
  direcciones de `notification_emails` (suscripciones de SNS que cada una confirma).
- **Red y cifrado.** La API publica por el interface endpoint de SNS, sin pasar por el NAT. La
  Lambda corre fuera de la VPC y hace el POST directo a `webhook_url`. Los topics y las colas
  se cifran con la clave del proyecto, cuya política habilita a SNS (escribe en la cola) y a
  CloudWatch (publica las alarmas en el topic).
- **Avisos a los titulares.** El topic `ledger-titulares` manda mails a cada titular; hoy, que
  su exportación está lista, con el link a la app y no al archivo. Al crear la cuenta, el
  frontend manda el ID token de Cognito (`X-Id-Token`); la API verifica su firma, que sea del
  mismo usuario y que el email esté verificado, y suscribe ese email con un *filter policy*
  `{"titular": ["<sub>"]}`. Cada aviso lleva el `sub` del titular como atributo, así que SNS
  se lo entrega sólo a esa suscripción. Se filtra por el `sub` de Cognito y no por el id de la
  cuenta porque el `sub` no se reutiliza: un reinicio de datos puede repetir ids de cuenta.
  El titular tiene que confirmar la suscripción con el mail que manda AWS. Lo natural para
  mails a usuarios sería SES, pero AWS Academy no lo habilita: el rol del laboratorio no tiene
  permisos de SES (`ses:GetAccount` y `ses:VerifyEmailIdentity` dan `AccessDenied`).

## 8. Frontend

Sitio estático en el bucket con website hosting: HTML, CSS y módulos de JavaScript sin
dependencias ni paso de build, que Terraform sube archivo por archivo junto con `config.json`.
Las decisiones que importan son de seguridad:

- **El token y las claves de operación viven en `sessionStorage`**, no en `localStorage`:
  duran lo que dura la pestaña y no quedan en el disco. Las claves de administración y del mock
  se borran al cerrar o perder la sesión, así que quien use la misma computadora después no
  hereda el acceso.
- **La pestaña Operación sólo aparece en modo operación**, con la clave de administración
  cargada. Ocultarla es comodidad, no control: el control lo hace la API, que exige
  `X-Admin-Key` en `/admin/*` y la compara en tiempo constante.
- **Sin `innerHTML`**: todo el texto que viene de la API se escribe con `textContent`, así que
  un alias con HTML se muestra como texto y no se ejecuta.
- **El saldo de bienvenida lo fija la API** (`SALDO_BIENVENIDA_CENTAVOS`), no el cliente.
- **Cada envío, retiro e ingreso lleva su `Idempotency-Key`**, la misma mientras no cambien los
  datos: si la red se corta después de que la API liquidó, reintentar no cobra dos veces.

Las operaciones que el proveedor no confirma enseguida (un `202`) se muestran como "en
revisión", no como éxito ni como error, y el comprobante ofrece actualizar el estado.

## 9. Estado

El state vive en S3, en `ledger-tfstate-<cuenta>-us-east-1-an`, con la key
`tp3/terraform.tfstate`. Tiene que ser remoto porque Deploy y Destroy corren en GitHub Actions:
con state local se perdería al terminar cada job, el siguiente apply intentaría crear todo de
nuevo y Destroy no tendría qué destruir.

- **Lo crea `make init`, no Terraform.** El backend necesita el bucket antes del `init`, así que
  no puede ser un recurso del mismo state. `make init` lo crea con la AWS CLI
  ([`scripts/buckets.sh`](scripts/buckets.sh)) si no existe (versionado, cifrado y
  sin acceso público, que S3 aplica por defecto) y le pasa el nombre al backend con
  `-backend-config`. Por eso [`backend.tf`](terraform/organization/backend.tf) no tiene
  `bucket`.
- **El nombre lleva la cuenta**, en el namespace regional de la cuenta (`-an`), igual que los
  otros dos buckets: el mismo código despliega en cualquier cuenta sin editar nada, y nadie
  más puede ocupar ese nombre.
- **Lock:** `use_lockfile = true`, el lock nativo del backend S3 (Terraform >= 1.10). El
  esquema clásico, S3 más una tabla de DynamoDB para el lock, quedó deprecado desde Terraform
  1.11 a favor de éste, así que no hace falta crear ni pagar esa tabla. El `plan` de los pull requests corre con `-lock=false`: no
  escribe el state, y así no espera a un deploy en curso.
- **Destroy lo borra al final**, con todas sus versiones, pero sólo si `terraform state list`
  quedó vacío: si el destroy falló a la mitad, el bucket queda para poder reintentar. El
  próximo `make init` (Deploy o el plan de un pull request) lo vuelve a crear.
- **Los buckets del proyecto también los crea la CLI.** El del archivo y el del frontend no
  son `aws_s3_bucket`: el SCP de AWS Academy niega `s3:GetBucketObjectLockConfiguration`, y el
  provider de AWS lo lee cada vez que refresca un `aws_s3_bucket`, así que el apply no puede
  terminar. `make apply` los crea con el mismo script justo antes del `terraform apply` (el
  `plan` de los pull requests no crea nada), y `storage` y `website` arman el mismo nombre y
  configuran todo lo demás: versionado, cifrado, bloqueo público, política, lifecycle y
  website, que no leen el object lock. `make destroy` los vacía, con todas sus versiones, y los
  borra junto con el del state.

## 10. Trade-offs

- **Un solo NAT**, en la primera AZ. Ahorra el costo de uno por AZ; si esa AZ se cae, las
  tareas de la API de la otra siguen atendiendo y llegando a la base y a los servicios de AWS
  (por los endpoints), pero no al proveedor ni a las claves públicas de Cognito. El webhook no
  depende del NAT: la Lambda corre fuera de la VPC. Las
  tareas que ya tenían las claves siguen validando sesiones; una tarea nueva no puede hasta que
  vuelva el NAT. Con el proveedor por defecto, el mock de la propia API, las operaciones con el
  proveedor tampoco se completan, porque la API lo llama por el NLB.
- **Ocho interface endpoints**, en las dos AZ: unos USD 0,16 por hora, más de tres veces el NAT.
  Son el precio de que el archivador no tenga ninguna salida a Internet: la alternativa barata
  es darle ruta al NAT, y entonces el único componente que borra datos del ledger podría
  mandarlos a cualquier lado. También dejan las instancias de la API independientes del NAT
  para hablar con ECS, ECR y Logs, y a las tareas para publicar los eventos en SNS.
- **La API en HTTP, sin TLS.** El laboratorio no tiene un dominio propio para emitir un
  certificado. El access token y las claves de operación (`X-Admin-Key`, `X-Mock-Key`) viajan
  en claro entre el navegador y el NLB; el login, en cambio, es HTTPS de punta a punta (Hosted
  UI y API Gateway).
- **Los secretos siguen en el state de Terraform**, porque los genera Terraform. El state está
  en un bucket privado y cifrado.
- **`LabRole` para todo**: las tareas, el agente de ECS y las Lambdas tienen los permisos del rol
  del laboratorio, no unos mínimos por componente. Por eso la separación de custodios de la
  clave KMS no es real: en una cuenta propia cada servicio tendría su rol y la política de la
  clave nombraría sólo a los que la necesitan.
- **Los buckets del proyecto quedan fuera del state** (ver [Estado](#9-estado)). Terraform no
  se entera si alguien borra uno a mano, `terraform apply` sin `make apply` falla porque no
  existen, y el nombre se arma en dos lugares: el script y los módulos. Para que no se
  desalineen, el `Makefile` define `PROJECT` una sola vez y se lo pasa a los dos (al script y a
  Terraform como `TF_VAR_project_name`).
- **RDS Multi-AZ** aunque duplique el tiempo del primer apply: la base es lo único con estado y
  sigue disponible si se cae una zona. `db_multi_az = false` la deja en una sola AZ para probar.
- **La base no sobrevive a Destroy**: sin deletion protection ni snapshot final, porque Destroy
  tiene que poder borrarla sin un apply previo. Mientras está desplegada guarda 7 días de backups
  automáticos, que permiten volver a cualquier punto de ese período (por ejemplo, después de un
  reinicio del ledger).
- **Sin X-Ray en las Lambdas, Performance Insights en RDS ni VPC Flow Logs**, por el laboratorio:
  el usuario de AWS Academy no puede leer las trazas de X-Ray ni las métricas de Performance
  Insights (los dos se pueden activar, pero nadie vería lo que juntan), y la trust policy de
  `LabRole` no incluye a `vpc-flow-logs.amazonaws.com`, así que los Flow Logs sólo podrían ir a
  S3, a un bucket aparte.
- **El evento se publica después del commit.** Si la tarea muere entre el commit y la
  publicación, ese aviso se pierde. La ventana es una llamada a SNS, y al apagarse la tarea la
  API espera las publicaciones en curso. Un outbox (el evento escrito en la misma transacción)
  lo evitaría, a costa de tocar la transacción de cada operación.
- **Sin autenticación IAM en RDS**: la API entra con la credencial de Secrets Manager, y
  habilitarla sólo sumaría una vía de login que nadie usa.
- **Mails a titulares por SNS y no por SES.** SNS admite 200 *filter policies* por topic (10.000
  por cuenta), así que pasados 200 titulares las suscripciones nuevas fallan y esos titulares
  quedan sin aviso (la cuenta se crea igual). Cada titular confirma su suscripción, los mails
  salen de `no-reply@sns.amazonaws.com` en texto plano y traen un link para desuscribirse. En una
  cuenta propia los avisos irían por SES.
- **El archivador escala en una sola instancia.** Paraleliza las partes de un mes dentro de una
  tarea. Si un mes no entrara en la ventana, el paso siguiente es un job de Glue (Spark) que lea
  la partición por JDBC en paralelo desde una réplica de lectura y escriba estos mismos archivos.
  Leer de la primaria con muchas conexiones competiría con la liquidación, y el standby
  Multi-AZ no acepta lecturas.
- tfsec marca estas decisiones; cada una está ignorada en el código con el motivo al lado.

## 11. Correcciones de entregas anteriores

| Entrega | Corrección | Qué se hizo |
|---|---|---|
| TP1 | Avanzar con la propuesta 2, el núcleo transaccional elástico | Es este sistema |
| TP1 | Un sistema financiero asíncrono obliga a lidiar con concurrencia y consistencia eventual | La concurrencia se resuelve en la base: bloqueo de fila sobre `saldos` en orden de id, idempotencia por restricción única y transacciones que escriben asientos y saldos juntos (ver [Datos](#4-datos)). La liquidación en sí es síncrona, dentro del request: el caso que fija la latencia es un pago con una persona esperando, y una cola agregaba más espera que la que ahorraba. Lo que sí es asíncrono es lo que depende de afuera: los egresos que el proveedor no confirma quedan en duda y se resuelven por consulta, y el archivado corre aparte, por horario |
| TP2 | Por la naturaleza de la aplicación, usar un network load balancer y no un application load balancer | NLB con targets `ip`. La justificación (latencia, picos sin calentamiento, conexiones largas, ISO 8583 sobre TCP, TLS de punta a punta e IP fija) está en [Entrada: por qué un NLB](#entrada-por-qué-un-nlb). La API escala por CPU, porque la métrica de requests por target es sólo del ALB |

## 12. Requisitos del TP

| Requisito | Dónde en este repo |
|---|---|
| ≥ 1 módulo externo | `terraform-aws-modules/vpc/aws ~> 6.7` en [`network.tf`](terraform/organization/network.tf) |
| ≥ 1 módulo propio | `security_groups`, `storage`, `website`, `auth`, `database`, `load_balancer`, `capacity`, `service`, `events`, `analytics` (10) |
| ≥ 4 funciones | `cidrsubnet`, `slice`, `range`, `substr`, `floor`, `sum`, `coalesce`, `concat`, `replace`, `try`, `format`, `jsonencode`, `toset`, `fileset`, `regex`, `filemd5`, `urlencode`, `templatefile`, `base64encode`, `sort`, `nonsensitive`, entre otras (ver el [README](README.md#funciones-utilizadas)) |
| ≥ 3 meta-argumentos | `for_each`, `count`, `depends_on` y `lifecycle` (ver el [README](README.md#meta-argumentos-utilizados)) |
| Variables y outputs | [`variables.tf`](terraform/organization/variables.tf) parametriza red, base, cómputo, proveedor y ventana del archivador; [`outputs.tf`](terraform/organization/outputs.tf) expone las URL y lo necesario para operar |
| Pipeline (init, validate, plan, apply) | Workflows **Terraform** y **Deploy** en [`.github/workflows`](.github/workflows) |
| Diagrama actualizado | [`img/arquitectura.png`](img/arquitectura.png), con su fuente editable en [`img/arquitectura.drawio`](img/arquitectura.drawio) |
| Correcciones previas | [Sección 11](#11-correcciones-de-entregas-anteriores) |
| Al menos un SQS o SNS (TP4) | [Eventos y notificaciones](#7-eventos-y-notificaciones): SNS, SQS con DLQ y Lambda |
