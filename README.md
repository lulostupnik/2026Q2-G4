# Núcleo Transaccional Elástico — TP4 (Terraform)

Cloud Computing · ITBA · 2026Q2 · Grupo 4

Núcleo transaccional de un PSP (proveedor de servicios de pago) desplegado en AWS con Terraform.
Cada usuario tiene una cuenta virtual con la que transfiere a otras cuentas, ingresa y retira
dinero hacia el exterior a través de un proveedor; cada operación se liquida dentro del mismo
request contra un ledger de partida doble en PostgreSQL, y los meses viejos se archivan en S3.
Cada orden terminada se publica como evento en SNS, y de ahí sale el webhook hacia otro sistema.

La arquitectura detallada, con el porqué de cada decisión y cómo se aplicaron las correcciones de
las entregas anteriores, está en [`ARCHITECTURE.md`](ARCHITECTURE.md).

![Diagrama de arquitectura en AWS](img/arquitectura.png)

**Flujo de datos:**

1. El navegador baja el frontend del bucket público de S3 (`website_url`) y su `config.json`,
   que Terraform genera con las URL de la API y del login.
2. El login pasa por la Hosted UI de Cognito, que vuelve a un callback HTTPS en API Gateway: una
   Lambda canjea el `code` por tokens y redirige al frontend con ellos.
3. El frontend llama a la API con el access token por el NLB público. El NLB reparte entre las
   tareas de la API, que corren en ECS sobre EC2 en las subredes de `app` de las dos AZ.
4. La API valida el token contra Cognito y liquida cada operación en RDS PostgreSQL Multi-AZ, en
   las subredes de `data`.
5. Los ingresos y egresos hacia el exterior van al proveedor por el NAT. Por defecto es el
   proveedor simulado que trae la propia API en `/servicio-mock`.
6. Cuando una orden termina, la API publica un evento en el topic SNS `ledger-events` por el
   interface endpoint de SNS. El topic lo deja en la cola SQS `ledger-webhook`, y una Lambda lo
   firma y lo manda a `webhook_url`. Lo que agota los reintentos queda en una DLQ, cuya alarma
   avisa por mail (topic `ledger-alerts`).
7. Todos los días, en la ventana de archivado, ECS levanta una tarea del archivador en las
   subredes de `archive`, que no tienen salida a Internet. Mueve a S3 (`transaction-history/`)
   los meses que superan la retención, por el gateway endpoint, en archivos Parquet partidos por
   rangos de cuentas.
8. El extracto de un mes archivado lo arma la API leyendo de S3 sólo las páginas del archivo que
   tienen esa cuenta. Las exportaciones del histórico se arman en segundo plano en `exports/`, se
   descargan con una URL prefirmada de S3 y el titular recibe un mail cuando están listas
   (topic `ledger-titulares`).
9. Los meses archivados se consultan con SQL desde Athena: Glue cataloga `transaction-history/`
   como la tabla `asientos`, y los resultados quedan cifrados en `athena-results/`.

Toda la operación (deploy, plan y destrucción) se hace desde **GitHub Actions**: el workflow
**Deploy** aplica todo el Terraform en la cuenta de AWS cuyos secrets tenga el repositorio, y
**Destroy** lo borra todo.

---

## Prerrequisitos

**Desde GitHub Actions** (el camino recomendado) no hace falta nada instalado: alcanza con una
cuenta de AWS Academy y los secrets del paso 1. El laboratorio ya trae `LabRole` y
`LabInstanceProfile`, que es lo que usa el despliegue porque no deja crear IAM.

**Desde una máquina local**, además:

| Herramienta | Versión / nota |
|---|---|
| Terraform | `>= 1.10, < 2.0` (ver [`versions.tf`](terraform/organization/versions.tf)); el pipeline usa 1.16.6 |
| AWS CLI | v2 reciente (`--bucket-namespace` en `create-bucket`) |
| Docker con `buildx` | `make imagen` construye la imagen del backend para amd64 y arm64 |
| Git | Calcula el tag de la imagen del backend (el hash de `apps/ledger`) |
| Make | Ejecuta los comandos del Makefile |
| jq | Lo usa el script que borra los buckets, y sirve para consultar los secretos |

---

## Integrantes

| Nombre | Legajo |
|---|---|
| Agustín Alonso | 63316 |
| Luciano Stupnik | 64233 |
| Tomás Becerra | 63732 |
| Valentino Sanguinetti | 63098 |
| Hannah Hauge Kalager | 69482 |

---

## Pasos a seguir

### 1. Configurar secrets

En el repositorio, **Settings → Secrets and variables → Actions → New repository secret**:

![Settings → Secrets and variables → Actions: los tres secrets de AWS y dos opcionales](img/secrets.png)

En la captura, el recuadro sólido marca los obligatorios y el punteado dos de los opcionales
(ver abajo).

| Secret | Obligatorio | Para qué |
|---|---|---|
| `AWS_ACCESS_KEY_ID` | Sí | Access key del laboratorio de AWS Academy |
| `AWS_SECRET_ACCESS_KEY` | Sí | Secret access key del laboratorio |
| `AWS_SESSION_TOKEN` | Sí | Session token del laboratorio |

Los tres salen de **AWS Details → AWS CLI** en el laboratorio y vencen con la sesión: hay que
volver a cargarlos cada vez que se inicia el lab, antes de correr un workflow.

#### Emails de notificación

En la pestaña **Variables**, `NOTIFICATION_EMAILS`: emails separados por comas (por ejemplo
`ana@gmail.com,beto@itba.edu.ar`). Cada uno se suscribe al topic SNS de alertas de operación
(la alarma de la DLQ del webhook). Después del primer Deploy, a cada dirección le llega un mail
de **AWS Notifications** con el link para confirmar la suscripción. Si queda vacía, no hay
alertas por mail.

#### Opcional: tamaño de los archivos del archivo

En la pestaña **Variables**, `ARCHIVE_ROWS_PER_PART` (5.000.000 si queda vacía) y
`ARCHIVE_ROWS_PER_ROW_GROUP` (1.000.000) fijan cuántas filas lleva cada archivo Parquet de un mes
archivado y cada bloque dentro de él. Con los datos de una demo, un mes entra en un solo archivo
de un solo bloque; para ver el mes partido en varios archivos y bloques, bajarlas (por ejemplo a
`8` y `3`) antes del Deploy.

#### Opcional: conectar el despliegue con el de otro equipo

Sin nada de esto, el despliegue funciona solo: los ingresos y egresos van al proveedor simulado
que trae la propia API (`/servicio-mock`) y no se avisa a nadie cuando termina una orden. Hay
dos conexiones posibles con otro despliegue, independientes entre sí:

- **Proveedor.** Un equipo manda sus ingresos y egresos al `/servicio-mock` del otro, en vez de
  al suyo. El que hace de proveedor no cambia nada de su lado; sólo comparte su clave del mock.
- **Webhook.** Cada vez que una orden termina, la API publica un evento en SNS y una Lambda le
  manda un aviso firmado (cabecera `X-Ledger-Signature`) a `<api_url del otro>/webhooks/recibidos`. El que recibe verifica la
  firma con el mismo secreto, y el aviso aparece en su pestaña *Operación*.

Los valores se cargan en la misma pantalla que los de AWS. Los workflows Deploy y Terraform se
los pasan a Terraform como `TF_VAR_*`:

| Nombre | Pestaña | Para qué | Si queda vacío |
|---|---|---|---|
| `MOCK_API_KEY` | Secrets | La clave que exige **mi** `/servicio-mock`. Si otro equipo me usa de proveedor, se la paso | Se genera una, distinta en cada despliegue desde cero |
| `PROVEEDOR_URL` | Variables | Adónde mando **mis** ingresos y egresos: `<api_url del otro>/servicio-mock` | Mi propio `/servicio-mock` |
| `PROVEEDOR_API_KEY` | Secrets | La clave del proveedor al que llamo: el `MOCK_API_KEY` del otro | La de mi propio mock |
| `WEBHOOK_URL` | Variables | Adónde la Lambda del webhook avisa **mis** órdenes terminadas: `<api_url del otro>/webhooks/recibidos` | La Lambda descarta los avisos |
| `WEBHOOK_SECRET` | Secrets | Con qué firmo los avisos que mando y verifico los que recibo. Tiene que ser igual en los dos despliegues | Se genera uno |

Por ejemplo, si el equipo B usa al equipo A de proveedor y los dos se avisan las órdenes:

| | Equipo A (proveedor) | Equipo B |
|---|---|---|
| `MOCK_API_KEY` | `clave-a` (inventada y pasada a B) | vacío |
| `PROVEEDOR_URL` | vacío | `<api_url de A>/servicio-mock` |
| `PROVEEDOR_API_KEY` | vacío | `clave-a` |
| `WEBHOOK_URL` | `<api_url de B>/webhooks/recibidos` | `<api_url de A>/webhooks/recibidos` |
| `WEBHOOK_SECRET` | `secreto-ab` | `secreto-ab` |

La `api_url` de cada uno (`http://<DNS del NLB>`) recién existe después del primer Deploy, así
que el orden es:

1. Acordar antes `MOCK_API_KEY` y `WEBHOOK_SECRET` (por ejemplo, con `openssl rand -hex 32`) y
   cargarlos en los dos repositorios. Fijarlos evita que cambien al destruir y volver a
   desplegar.
2. Los dos corren **Deploy** sin URL y se pasan la `api_url` que queda en el resumen del run.
3. Cada uno carga `PROVEEDOR_URL` y/o `WEBHOOK_URL` con la `api_url` del otro y vuelve a correr
   **Deploy**. Este segundo Deploy no toca el NLB, así que la `api_url` no cambia. La
   `WEBHOOK_URL` sólo actualiza la Lambda del webhook, sin reemplazar las tareas de la API.

Para el proveedor, el segundo Deploy se puede evitar: la tarjeta de proveedor de *Operación*
(o `POST /admin/proveedor`) cambia la URL en caliente, y el cambio queda guardado en la base
hasta *Volver al de siempre*. La clave no se cambia ahí: sale siempre de `PROVEEDOR_API_KEY`.

Para comprobar que las claves coinciden sin mostrarlas, `GET /admin/diagnostico` (la tarjeta de
diagnóstico de *Operación*) muestra una huella de cada una: la de `mock_api_key` de A tiene que
ser igual a la de `proveedor_api_key` de B, y la de `webhook_secret`, igual en los dos.

Desde una máquina local, los mismos valores van en `terraform/organization/terraform.tfvars`
o como variables de entorno `TF_VAR_*` antes de `make plan`. El resto de las variables (tipos de
instancia, tamaño de la base, filas por archivo del archivo, días que quedan las exportaciones)
tienen valores por defecto y se pueden cambiar igual; están, con sus defaults, en
[`terraform.tfvars.example`](terraform/organization/terraform.tfvars.example).

### 2. Workflows configurados

![Pestaña Actions con los workflows Deploy, Destroy y Terraform](img/workflows.png)

| Workflow | Cuándo corre / confirmación | Qué hace |
|---|---|---|
| **Terraform** | En cada pull request, o a mano | `fmt -check`, `init -backend=false` y `validate` (sin AWS); después `init` y `plan` contra la cuenta. El resultado del plan queda en el resumen del run |
| **Deploy** | A mano, escribiendo `deploy` | `make imagen` (construye la imagen del backend y la sube a ECR; el tag es el hash de `apps/ledger`, así que sólo cambia si cambia el backend), `make plan` y `make apply`. Las URL del despliegue quedan en el resumen del run |
| **Destroy** | A mano, escribiendo `destroy` | `terraform destroy` de toda la infraestructura y después los buckets (archivo, frontend y state) |

Deploy y Destroy comparten un grupo de `concurrency`: nunca corren a la vez sobre el mismo state.

En un pull request que viene de un fork o de dependabot, GitHub no le pasa los secrets al
workflow: ahí
**Terraform** corre `fmt`, `init` y `validate`, y el `plan` aparece como salteado (*skipped*)
en lugar de fallar por falta de credenciales. El `plan` contra la cuenta corre en los pull
requests dentro del mismo repositorio, al lanzar **Terraform** a mano y en cada **Deploy**. Si
el repositorio no tiene los secrets de AWS (o la sesión del laboratorio venció y nadie los
renovó), en un pull request el `plan` también se saltea con un aviso en el resumen del run.

Los workflows que ya traía el repositorio (`terraform-format`, `terraform-docs`, `tfsec`)
corren en paralelo en cada pull request. `terraform-docs` regenera los README de
`terraform/modules/` y trata de commitearlos en la rama del PR, para lo que necesita que el
token del workflow tenga permiso de escritura (un PR desde un fork no lo tiene). Los README de
los módulos ya se commitean generados con terraform-docs, así que no debería encontrar cambios.

### 3. Desplegar

1. Con los secrets cargados: **Actions → Deploy → Run workflow** → rama `main` → escribir
   `deploy`. Mergear no despliega nada, a propósito: las credenciales del laboratorio vencen con
   cada sesión, y un apply que crea RDS Multi-AZ y gasta crédito tiene que ser una decisión
   explícita, no un efecto de mergear. El `plan` sí corre en los pull requests.

   **Desde otra cuenta** (por ejemplo, la de la cátedra): Deploy se lanza a mano y usa los
   secrets del repositorio donde corre, así que hace falta un repositorio propio con este código,
   por ejemplo un fork, con **Actions** habilitado (en un fork nuevo, GitHub lo deja apagado
   hasta tocar *I understand my workflows*) y los tres secrets de AWS cargados. Todos los
   nombres llevan el ID de la cuenta, así que no hay que editar nada.

   ![Workflow Deploy: Run workflow con la confirmación deploy](img/deploy.png)

2. Esperar el run en verde. El primer apply tarda más que los siguientes: RDS Multi-AZ sola
   lleva varios minutos.
3. Abrir el resumen del run: tiene `website_url` (frontend), `api_url` y `login_url`.

   ![Resumen del run de Deploy con las URL del despliegue](img/resumen.png)

### 4. Probar

Con las URL del resumen del run, verificar que la API responde, con el estado del proceso y de
la base:

```bash
curl <api_url>/health
```

El endpoint devuelve HTTP 200 incluso si la base no responde.
Para comprobar la conexión a PostgreSQL, verificar que el campo
`base` del JSON sea `"ok"`; si falla, devuelve `"sin_respuesta"`
y `estado` pasa a `"degradado"`.

Después, entrar por `website_url`:

![Pantalla de ingreso del sitio](img/login.png)

1. **Ingresar** lleva a la Hosted UI de Cognito. La primera vez, **Sign up** con un email y una
   contraseña (8 caracteres, con minúsculas y números). Cognito manda un código a ese email; al
   cargarlo se vuelve al sitio con la sesión iniciada. El registro está abierto, así que no hace
   falta crear usuarios antes.

   ![Hosted UI de Cognito: Sign in y Sign up](img/cognito.png)

   Si el código no llega (Cognito manda los emails con su remitente por defecto, que tiene un
   límite diario), el usuario se confirma por CLI. El `user_pool_id` está en `terraform output
   cognito`:

   ```bash
   aws cognito-idp admin-confirm-sign-up --user-pool-id <user_pool_id> --username <email>
   ```

2. La primera vez pide un alias y crea la cuenta. Arranca con $ 50.000 de saldo de bienvenida,
   que fija la API (`SALDO_BIENVENIDA_CENTAVOS`) y se asienta contra la cuenta interna de
   ajustes: el cliente no puede elegir con cuánto abre su cuenta.
3. La billetera:
   - **Inicio**: saldo (se puede ocultar), alias para compartir y movimientos recientes.
   - **Enviar**: elegir a quién (con buscador), el monto (valida contra el saldo) y confirmar.
   - **Ingresar**: el monto y un código para usar como concepto de la transferencia; *Ya hice
     la transferencia* simula el aviso del banco.
   - **Retirar**: CBU o CVU de 22 dígitos, el monto y confirmar. Sale por el proveedor: un
     monto terminado en 13 centavos lo rechaza y uno terminado en 99 queda *en revisión* hasta
     que la API lo resuelve por consulta.
   - **Actividad**: mes por mes, con lo que entró, lo que salió y un filtro; el resumen de un
     mes archivado lo arma la API desde S3.
   - Cada operación tiene su comprobante, con el número de operación y su estado.

   ![Inicio de la billetera: saldo, alias y movimientos recientes](img/inicio.png)

   ![Enviar: confirmación con el resumen antes de mandar el dinero](img/enviar.png)

   ![Retirar: confirmación con la cuenta de destino y lo que queda](img/retirar.png)

   ![Actividad: el mes con lo que entró y salió, agrupado por día](img/actividad.png)

4. **Operación** (salud y qué tarea atendió, proveedor activo, cierre contable, archivado,
   extracto y exportación, y la cinta de llamadas a la API con su código y su latencia) no la
   ve un usuario común: aparece al activar el *Modo operación* desde el menú de la cuenta, con
   la clave de administración (y la del mock para sus tarjetas). Las claves quedan sólo en esa
   pestaña del navegador (`sessionStorage`) y se borran al cerrar sesión. Están en el secreto
   `ledger/api` de Secrets Manager:

   ```bash
   aws secretsmanager get-secret-value --secret-id ledger/api --query SecretString --output text \
     | jq '{ADMIN_API_KEY, MOCK_API_KEY}'
   ```

   ![Operación: estado del despliegue, base y tarea que atendió el pedido](img/operacion.png)

5. **Eventos.** Con `WEBHOOK_URL` cargada, cada orden terminada aparece en *Avisos recibidos*
   del otro despliegue, con la firma verificada. Para verlos sin otro equipo, apuntar
   `WEBHOOK_URL` a la propia API (`<api_url>/webhooks/recibidos`). Lo que hace la Lambda queda
   en `/aws/lambda/ledger-webhook` de CloudWatch Logs.

   Para ver la DLQ, apuntar `WEBHOOK_URL` a una ruta que no existe (por ejemplo
   `<api_url>/webhooks/no-existe`), volver a correr Deploy y hacer una transferencia. SQS
   reintenta una vez por minuto; al quinto fallo el aviso pasa a `ledger-webhook-dlq` y a las
   direcciones de `NOTIFICATION_EMAILS` les llega la alarma `ledger-webhook-dlq`. Después,
   volver a la URL correcta y vaciar la DLQ (**SQS → ledger-webhook-dlq → Purge**).

6. **Análisis del archivo con Athena.** Athena sólo ve los meses archivados, así que primero
   tiene que haber alguno. Desde la tarjeta *Cierre contable y archivado* de *Operación*:
   **Sembrar mes anterior** (necesita otra cuenta de usuario y saldo en la propia) y después
   **Agendar archivado**. El archivador lo sube a S3 en su próxima ventana (19:25 a 21:30,
   hora de Buenos Aires); para no esperarla, levantarlo a mano y volver a bajarlo cuando
   termine:

   ```bash
   aws ecs update-service --cluster ledger-cluster --service ledger-archive --desired-count 1
   aws ecs update-service --cluster ledger-cluster --service ledger-archive --desired-count 0
   ```

   Después, en la consola de Athena (`terraform output analytics` tiene la URL):

   1. En el editor, elegir el workgroup **`ledger`** (arriba a la derecha). Usa la base
      `ledger` y deja los resultados en `athena-results/`, cifrados; la configuración la fija
      el workgroup.
   2. En **Saved queries**, abrir y correr:

      | Consulta | Qué muestra |
      |---|---|
      | `partida-doble` | Por mes: asientos, órdenes y la suma, que tiene que dar 0 (`cuadra = true`) |
      | `volumen-mensual` | Órdenes y pesos movidos por mes y por tramo |
      | `cuentas-mas-activas` | Las 20 cuentas con más órdenes, con lo que entró y salió |
      | `movimientos-de-cuenta` | Los movimientos archivados de una cuenta; pide `cuenta_id` |

   O por CLI:

   ```bash
   aws athena start-query-execution --work-group ledger \
     --query-string "SELECT periodo, count(*) AS asientos, sum(monto_centavos) AS suma FROM ledger.asientos GROUP BY periodo"
   aws athena get-query-results --query-execution-id <id>
   ```

   Un mes recién archivado aparece en la próxima consulta, sin correr nada más: la tabla
   proyecta sus particiones (ver [Análisis del archivo](ARCHITECTURE.md#análisis-del-archivo-con-athena)).

   Para ver el archivo partido en varios Parquet con pocos datos, bajar `ARCHIVE_ROWS_PER_PART`
   y `ARCHIVE_ROWS_PER_ROW_GROUP` (ver [Configurar secrets](#1-configurar-secrets)) antes del
   Deploy. Cada parte queda en `transaction-history/periodo=AAAA-MM/parte-NNN.parquet`.

7. **Extractos y exportaciones del archivo.** Con un mes ya archivado, *Ver extracto* de ese mes
   en *Operación* muestra `Origen: archivo`: la API leyó de S3 sólo las páginas de esa cuenta.
   *Exportar* registra la exportación y la arma en segundo plano; la pantalla la consulta hasta
   que queda `lista` y entonces muestra **Descargar**, que pide una URL prefirmada nueva (vence a
   los 15 minutos) y baja el archivo directo de S3.

   **Aviso por mail.** Al crear la cuenta, la API suscribe el email del usuario de Cognito al
   topic `ledger-titulares`. AWS manda un mail *AWS Notification - Subscription Confirmation*:
   hay que confirmarlo, y desde ahí cada exportación lista llega por mail con el link a la app.

### 5. Destruir al terminar

**Actions → Destroy → Run workflow** → escribir `destroy`.

![Workflow Destroy: Run workflow con la confirmación destroy](img/destroy.png)

Borra todos los recursos y, cuando el state quedó vacío, también los buckets (ver
[Estado](ARCHITECTURE.md#9-estado)). Sólo quedan la clave KMS, que KMS borra a los 7 días, y
el log group que crea Container Insights (`/aws/ecs/containerinsights/ledger-cluster/performance`,
con retención de un día); ninguno de los dos molesta al próximo Deploy.

### Desde una máquina local

Con las credenciales del laboratorio en `~/.aws/credentials` (o en variables de entorno), los
mismos pasos que el pipeline:

```bash
make init      # crea el bucket del state si no existe e inicializa terraform/organization
make imagen    # construye apps/ledger y la sube a ECR (si ese tag ya está, no hace nada)
make plan      # deja el plan en terraform/organization/tfplan, con ese tag
make apply     # crea los buckets del proyecto si no existen y aplica ese plan
make destroy   # pide confirmación; borra también los buckets
```

`make help` lista el resto (`fmt`, `fmt-check`, `validate`, `clean`). Local y pipeline usan el
mismo state, así que lo desplegado desde un lado se ve y se destruye desde el otro.

---

## Buckets de S3: los crea la AWS CLI

Los tres buckets del proyecto no son recursos `aws_s3_bucket`: los crea
[`scripts/buckets.sh`](scripts/buckets.sh) con `aws s3api create-bucket`, tanto en el pipeline
como desde una máquina local.

| Bucket | Lo crea | Por qué no Terraform |
|---|---|---|
| `ledger-<cuenta>-us-east-1-an` (archivo y exportaciones) | `make apply`, antes del `terraform apply` | El SCP de AWS Academy |
| `ledger-web-<cuenta>-us-east-1-an` (frontend) | `make apply`, antes del `terraform apply` | El SCP de AWS Academy |
| `ledger-tfstate-<cuenta>-us-east-1-an` (state) | `make init` | El backend lo necesita antes del `init` |

**El SCP de AWS Academy.** La política de la organización del laboratorio niega
`s3:GetBucketObjectLockConfiguration` sobre cualquier bucket. El provider de AWS hace esa
lectura cada vez que lee un `aws_s3_bucket`, después de crearlo y en cada refresh, y no tiene
ningún argumento para saltearla: el bucket se crea, pero el apply falla y el recurso queda
*tainted*. Por eso el bucket lo crea la CLI, y Terraform administra todo lo demás con los
recursos que no leen el object lock: versionado, cifrado, bloqueo de acceso público, política,
lifecycle, website y los objetos. Los módulos `storage` y `website` arman el mismo nombre que el
script.

**El bucket del state** se crea aparte por otro motivo: el backend S3 tiene que existir antes de
`terraform init`, así que no puede ser un recurso del mismo state (ver
[Estado](ARCHITECTURE.md#9-estado)).

`make destroy` (y el workflow **Destroy**) borra los tres buckets con todas sus versiones, pero
sólo cuando `terraform destroy` dejó el state vacío. En una cuenta sin ese SCP, los dos buckets
del proyecto serían un `aws_s3_bucket` dentro de cada módulo y el script sólo crearía el del
state.

---

## Módulos

La composición raíz es [`terraform/organization`](terraform/organization): instancia los módulos
y define lo que no pertenece a ninguno: los endpoints de la VPC, el repositorio de ECR y el
cluster de ECS ([`ecs.tf`](terraform/organization/ecs.tf)), las claves generadas y sus secretos
de Secrets Manager ([`secrets.tf`](terraform/organization/secrets.tf)) y la clave KMS del
proyecto ([`kms.tf`](terraform/organization/kms.tf)). Cada módulo propio tiene su README con
las variables y los outputs.

### `terraform-aws-modules/vpc/aws` (externo)

VPC `10.0.0.0/16` con 8 subredes en 4 capas × 2 AZ: `public` (NLB y NAT), `app` (API e
interface endpoints), `archive` (archivador, sin salida a Internet) y `data` (RDS). Crea
también el IGW, un NAT, las tablas de ruteo de cada capa y el subnet group de la base. Los CIDR
y los nombres de las subredes se derivan en [`locals.tf`](terraform/organization/locals.tf).

**Recursos propios junto al módulo** ([`network.tf`](terraform/organization/network.tf)):

- `aws_vpc_endpoint.s3`: gateway endpoint de S3 en las tablas de `app` y `archive`, con una
  política que sólo deja llegar al bucket del proyecto y al de las capas de ECR.
- `aws_vpc_endpoint.interface`: un interface endpoint por servicio que necesitan el agente de
  ECS (`ecs`, `ecs-agent`, `ecs-telemetry`, `ecr.api`, `ecr.dkr`, `logs`, `secretsmanager`) y la
  API para publicar los eventos (`sns`), en las subredes de `app`, que sólo acepta credenciales
  de la propia cuenta.

**Versión:** `~> 6.7`.

---

### [`modules/security_groups`](terraform/modules/security_groups)

Un security group por capa (`nlb`, `app`, `archive`, `data`), más los de los hosts de ECS
(`host`) y los interface endpoints (`endpoints`). Las reglas salen de una sola tabla en
`locals` y referencian grupos o la prefix list de S3, no rangos: sólo el NLB acepta tráfico de
Internet y sólo la API sale a Internet.

**Recursos:** `aws_security_group.this`, `aws_vpc_security_group_ingress_rule.this` y
`aws_vpc_security_group_egress_rule.this`.

---

### [`modules/storage`](terraform/modules/storage)

Bucket privado del proyecto: el archivo del ledger (`transaction-history/`), las exportaciones
(`exports/`) y los resultados de Athena (`athena-results/`). Versionado, cifrado con la clave
KMS del proyecto (SSE-KMS con bucket key), con bloqueo de acceso público y una política que
rechaza cualquier pedido que no venga por HTTPS. El lifecycle borra las versiones reemplazadas
a los 30 días, pasa el archivo a STANDARD_IA a los 30 días y a GLACIER Instant Retrieval al año
(Athena no lee GLACIER Flexible Retrieval), expira los resultados de Athena a los 7 días y las
exportaciones a los `export_retention_days` (con el versionado, se borran del todo 30 días
después), y aborta las subidas multipart que quedan cortadas, para no pagar sus partes.

El bucket lo crea `make apply` con la AWS CLI y el módulo lo configura (ver
[Buckets de S3](#buckets-de-s3-los-crea-la-aws-cli)).

**Recursos:** `aws_s3_bucket_versioning.this`,
`aws_s3_bucket_server_side_encryption_configuration.this`,
`aws_s3_bucket_public_access_block.this`, `aws_s3_bucket_policy.this` y
`aws_s3_bucket_lifecycle_configuration.this`.

---

### [`modules/website`](terraform/modules/website)

Bucket público con website hosting (HTTP) para el frontend de [`apps/web`](apps/web). Sube un
objeto por archivo con su `Content-Type`, y publica `config.json` con lo que el frontend no
puede saber hasta el apply (`api_url`, `login_url`, `logout_url`). Se cifra con AES256 y no con
KMS: los usuarios anónimos del sitio no pueden tener permiso sobre una clave.

Como el de `storage`, el bucket lo crea `make apply` con la AWS CLI (ver
[Buckets de S3](#buckets-de-s3-los-crea-la-aws-cli)).

**Recursos:** `aws_s3_bucket_website_configuration.this`,
`aws_s3_bucket_policy.public_read`, `aws_s3_object.site` y `aws_s3_object.config`.

---

### [`modules/auth`](terraform/modules/auth)

User pool de Cognito con Hosted UI (login por email) y un app client con client secret. Como
Cognito sólo acepta callbacks HTTPS, suma una HTTP API de API Gateway con las rutas `/callback`
y `/logout` hacia una Lambda ([`apps/callback`](apps/callback)) que canjea el `code` por tokens
(ver [Login con Cognito](ARCHITECTURE.md#6-login-con-cognito)). El client secret está en Secrets
Manager (`ledger/callback`) y la Lambda sólo recibe su ARN. El stage de API Gateway manda un
access log JSON por request a su propio log group.

**Recursos:** `aws_cognito_user_pool.this`, `aws_cognito_user_pool_domain.this`,
`aws_cognito_user_pool_client.this`, `aws_secretsmanager_secret.client`,
`aws_lambda_function.callback`, `aws_apigatewayv2_api.this` y sus rutas, integración y stage.

---

### [`modules/events`](terraform/modules/events)

Topic SNS `ledger-events`, donde la API publica cada orden terminada, y una cola SQS con DLQ
suscripta a él, que lee la Lambda del webhook ([`apps/webhook`](apps/webhook)). Un topic de
alertas con suscripciones de email recibe la alarma de la DLQ, y el topic `ledger-titulares`
manda los avisos por mail a cada titular, con una suscripción por titular que filtra por su
`sub` de Cognito (ver [Eventos y notificaciones](ARCHITECTURE.md#7-eventos-y-notificaciones)).

**Recursos:** `aws_sns_topic.events`, `aws_sns_topic.titulares`, `aws_sns_topic.alerts`, `aws_sqs_queue.webhook` y
`webhook_dlq`, `aws_lambda_function.this` (webhook), `aws_lambda_event_source_mapping.webhook`,
`aws_cloudwatch_metric_alarm.webhook_dlq` y las suscripciones.

---

### [`modules/analytics`](terraform/modules/analytics)

Consultas SQL sobre el archivo, sin tocar RDS. Una base del catálogo de Glue con la tabla
externa `asientos` sobre `transaction-history/` (Parquet, particionada por `periodo` con
*partition projection*), un workgroup de Athena que fuerza dónde y cómo se guardan los
resultados (en `athena-results/`, SSE-KMS con la clave del proyecto) y pone un tope de bytes
escaneados por consulta, y cuatro consultas guardadas (ver
[Análisis del archivo](ARCHITECTURE.md#análisis-del-archivo-con-athena)).

**Recursos:** `aws_glue_catalog_database.this`, `aws_glue_catalog_table.asientos`,
`aws_athena_workgroup.this` y `aws_athena_named_query.this`.

---

### [`modules/database`](terraform/modules/database)

RDS PostgreSQL Multi-AZ en las subredes de `data`, cifrada con la clave KMS del proyecto y sin
acceso público. Expone la `DB_URL` de los binarios, con TLS obligatorio.

**Recursos:** `aws_db_instance.this`.

---

### [`modules/load_balancer`](terraform/modules/load_balancer)

NLB público con un listener TCP:80 y el target group de la API. Los targets son las IP de las
tareas, que ECS registra solo, y el health check es HTTP a `/health`.

**Recursos:** `aws_lb.this`, `aws_lb_target_group.api` y `aws_lb_listener.http`.

---

### [`modules/capacity`](terraform/modules/capacity)

Las instancias de un servicio de ECS: launch template con la AMI optimizada para ECS (sale de
SSM, según la arquitectura de la instancia), el disco raíz cifrado e IMDSv2 obligatorio, un ASG
y el capacity provider que lo escala según las tareas. Para la API se configura
`target_capacity = 80`, con el objetivo de mantener capacidad de reserva y reducir
la espera al iniciar nuevas tareas. Esto no garantiza un arranque inmediato:
puede ser necesario lanzar nuevas instancias.

**Recursos:** `aws_launch_template.this`, `aws_autoscaling_group.this` y
`aws_ecs_capacity_provider.this`.

---

### [`modules/service`](terraform/modules/service)

Un servicio de ECS: task definition con red `awsvpc`, el servicio con circuit breaker, el log
group cifrado y el autoscaling de las tareas, por CPU (la API) o por horario (el archivador).
La task definition separa las variables comunes de las secretas: éstas son referencias
(`valueFrom`) a Secrets Manager, y ECS lee el valor al arrancar la tarea (ver
[Secretos y cifrado](ARCHITECTURE.md#5-secretos-y-cifrado)).

**Recursos:** `aws_ecs_task_definition.this`, `aws_ecs_service.this`,
`aws_cloudwatch_log_group.this`, `aws_appautoscaling_target.this`,
`aws_appautoscaling_policy.cpu` y `aws_appautoscaling_scheduled_action.this`.

`capacity` y `service` se instancian una vez por servicio (API y archivador) con `for_each`
sobre `local.services`; ver [Cómputo](ARCHITECTURE.md#3-cómputo).

---

## Funciones y meta-argumentos de Terraform

### Funciones utilizadas

| Función | Dónde | Para qué |
|---|---|---|
| `cidrsubnet` | `organization/locals.tf` | Deriva los CIDR de cada capa desde el de la VPC |
| `slice` | `organization/locals.tf` | Toma las dos primeras AZ de la región |
| `range` | `organization/locals.tf` | Una subred por AZ dentro de cada capa |
| `substr` | `organization/locals.tf` | Saca la letra de la AZ para el nombre de la subred (`ledger-app-a`) |
| `floor`, `sum`, `values` | `organization/locals.tf` | Reparte `db_max_connections` entre las tareas: el tope de cada una va en la `DB_URL` (`pool_max_conns`) |
| `coalesce` | `organization/locals.tf` | Usa la clave o la URL que se pasó por variable y, si está vacía, la generada o la propia |
| `tostring` | `organization/locals.tf` | El puerto de la API y las filas por parte y por bloque del archivador como variables de entorno, que en la task definition son strings |
| `concat` | `organization/network.tf` | Asocia el endpoint de S3 a las tablas de app y archivado |
| `replace` | `organization/network.tf`, `modules/security_groups/main.tf`, `modules/database/main.tf`, `modules/analytics/main.tf` | Arma nombres: el `Name` de cada endpoint y de cada regla (`app_in_nlb` → `ledger-app-in-nlb`), y el de la base de RDS y la de Glue sin guiones |
| `try` | `modules/security_groups/main.tf` | Usa el SG de destino sólo si el `peer` de la regla es otro grupo |
| `format` | `modules/storage/main.tf`, `modules/website/main.tf`, `modules/auth/main.tf`, `modules/database/outputs.tf` | Arma los nombres de los buckets, la URL del dominio de Cognito y la `DB_URL` |
| `jsonencode` | `organization/network.tf`, `organization/kms.tf`, `organization/secrets.tf`, `modules/storage/main.tf`, `modules/website/main.tf`, `modules/service/main.tf`, `modules/auth/main.tf` | Las políticas de los endpoints, de la clave KMS y de los buckets, el contenido de cada secreto, el `config.json` del frontend, las `container_definitions` y el formato de los access logs |
| `nonsensitive` | `organization/main.tf` | Los nombres de las variables secretas no son secretos: así se pueden recorrer para armar cada `valueFrom` |
| `toset` | `organization/network.tf`, `organization/secrets.tf`, `modules/auth/main.tf` | Un elemento por endpoint, por clave generada y por ruta de API Gateway |
| `fileset` | `modules/website/main.tf` | Lista los archivos del frontend para subir uno por uno |
| `regex`, `lower`, `contains`, `keys` | `modules/website/main.tf` | Saca la extensión y publica sólo los tipos conocidos |
| `filemd5` | `modules/website/main.tf` | Hash de cada archivo: se vuelve a subir sólo si cambió |
| `length` | `modules/website/main.tf` | Publica `config.json` sólo si hay `runtime_config` |
| `urlencode`, `join` | `modules/auth/outputs.tf` | Arma las URL de login y logout de la Hosted UI |
| `templatefile` | `modules/capacity/main.tf` | El `user_data` de las instancias, con el nombre del cluster |
| `format`, `contains` | `modules/capacity/main.tf` | Elige el parámetro de SSM de la AMI arm64 si el tipo de instancia admite esa arquitectura |
| `base64encode` | `modules/capacity/main.tf` | El `user_data` va codificado en el launch template |
| `sort`, `keys` | `modules/service/main.tf` | Las variables de entorno en orden fijo: el plan no muestra cambios espurios |
| `alltrue`, `contains` | `organization/variables.tf` | Valida que el archivador vaya de 0 a 1 tarea |
| `contains` | `modules/auth/variables.tf`, `modules/service/variables.tf` | Valida que la retención de los logs sea una que acepta CloudWatch |
| `can`, `regex`, `endswith` | `modules/analytics/variables.tf` | Valida que `history_start` sea un mes `AAAA-MM` y que los prefijos terminen en `/` |

### Meta-argumentos utilizados

| Meta-argumento | Dónde | Para qué |
|---|---|---|
| `for_each` | `organization/main.tf` (`module "capacity"`, `module "service"`) | Instancias y servicio de ECS para la API y el archivador sin duplicar código |
| `for_each` | `organization/network.tf` | Un interface endpoint por servicio de AWS que necesitan el agente de ECS y la API |
| `for_each` | `organization/secrets.tf` | Las cuatro claves generadas (`db`, `admin`, `mock`, `webhook`) y un secreto de Secrets Manager por servicio |
| `for_each` | `modules/security_groups/main.tf` | Un SG por capa y una regla por fila de la tabla de reglas |
| `for_each` | `modules/website/main.tf` | Un objeto de S3 por archivo del frontend |
| `for_each` | `modules/auth/main.tf` | Las rutas `/callback` y `/logout` hacia la misma integración |
| `for_each` | `modules/events/main.tf` | La Lambda del webhook con su zip y su log group, y una suscripción de alertas por cada email de `notification_emails` |
| `for_each` | `modules/service/main.tf` | Una acción programada por cada entrada de `schedule` (abrir y cerrar la ventana del archivador) |
| `for_each` | `modules/analytics/main.tf` | Una consulta guardada de Athena por entrada de `local.queries` |
| `count` | `modules/website/main.tf` | Publica `config.json` sólo si hay `runtime_config` |
| `count` | `modules/service/main.tf` | La política de escalado por CPU sólo en los servicios que la piden (la API) |
| `depends_on` | `organization/main.tf` (`module "capacity"`) | Al destruir, las instancias y los servicios de ECS se borran antes que los endpoints y los security groups que usa su agente |
| `depends_on` | `modules/auth/main.tf` | El log group de la Lambda existe antes que ella, con retención y dentro del state |
| `depends_on` | `modules/events/main.tf` | La suscripción de SNS a la cola se crea después de la política que deja escribir a SNS |
| `depends_on` | `modules/storage/main.tf`, `modules/website/main.tf` | La política de cada bucket se aplica después de su bloqueo de acceso público |
| `depends_on` | `modules/storage/main.tf` | El lifecycle se aplica después del versionado, porque expira versiones anteriores |
| `lifecycle` | `modules/service/main.tf` | `ignore_changes = [desired_count]`: la cantidad de tareas la maneja el autoscaling, no Terraform |
| `lifecycle` | `modules/capacity/main.tf` | `ignore_changes = [desired_capacity]`: la cantidad de instancias la maneja el capacity provider |

### Validación de variables

Los bloques `validation` comprueban los valores de entrada: limitan `archive_schedule` a 0 o 1
tarea, `log_retention_days` a los valores que admite CloudWatch, `target_capacity` al rango de
1 a 100, que el bloque de un Parquet del archivo no tenga más filas que la parte
(`archive_rows_per_row_group` ≤ `archive_rows_per_part`, una validación que cruza dos variables)
y, en Athena, el formato de `history_start`, los prefijos y el tope de bytes escaneados (Athena
no acepta menos de 10 MiB). Están en `organization/variables.tf`,
`modules/auth/variables.tf`, `modules/service/variables.tf`, `modules/capacity/variables.tf` y
`modules/analytics/variables.tf`, y un valor inválido falla en el `plan`, no a mitad del apply.

### Bloques `dynamic`

| Dónde | Para qué |
|---|---|
| `modules/storage/main.tf` | Una regla de lifecycle por prefijo, con sus transiciones, y otra por cada prefijo que expira (`athena-results/`) |
| `modules/analytics/main.tf` | Las columnas de la tabla de Glue, desde una lista en `locals` |
| `modules/service/main.tf` | El bloque `load_balancer` del servicio sólo si tiene target group (la API sí, el archivador no) |

### Data sources

| Data source | Dónde | Para qué |
|---|---|---|
| `aws_availability_zones` | `organization/providers.tf` | Las AZ disponibles de la región |
| `aws_caller_identity` | `organization/providers.tf`, `modules/storage`, `modules/website`, `modules/auth` | La cuenta: va en los nombres de los buckets, en el dominio de Cognito y en las políticas de los endpoints y de la clave KMS |
| `aws_region` | `modules/storage`, `modules/website`, `modules/auth`, `modules/service` | La región, sin pasarla como variable a cada módulo |
| `aws_iam_role`, `aws_iam_instance_profile` | `organization/providers.tf` | `LabRole` y `LabInstanceProfile`: AWS Academy no deja crear IAM. El ARN de `LabRole` también va en la política de la clave KMS |
| `aws_ec2_instance_type` | `modules/capacity/main.tf` | La arquitectura del tipo de instancia (x86 o arm64) |
| `aws_ssm_parameter` | `modules/capacity/main.tf` | La AMI optimizada para ECS recomendada para esa arquitectura, sin fijar un ID |
| `archive_file` | `modules/auth/main.tf`, `modules/events/main.tf` | El zip de la Lambda del callback y el de la del webhook |

---

## Mapa del repositorio

```
.
├── .github/workflows/     # Terraform (validate + plan), Deploy, Destroy y los que ya traía el repo
├── Makefile               # init (con el bucket del state), imagen, plan, apply, destroy, fmt, validate
├── ARCHITECTURE.md        # red, cómputo, secretos y cifrado, login, eventos, state y trade-offs
├── img/                   # diagrama de arquitectura (.png y .drawio) y capturas de los pasos
├── scripts/
│   └── buckets.sh         # crea y borra los buckets: el del state y los del proyecto
├── terraform/
│   ├── organization/      # composición raíz: acá se corre terraform
│   └── modules/           # módulos propios
│       ├── security_groups/
│       ├── storage/       # bucket privado: archivo del ledger
│       ├── analytics/     # Glue + Athena: consultas SQL sobre el archivo
│       ├── website/       # bucket público: frontend
│       ├── database/
│       ├── load_balancer/
│       ├── auth/          # Cognito + API Gateway + Lambda: callback HTTPS del login
│       ├── events/        # SNS, SQS con DLQ, Lambda del webhook y alertas
│       ├── capacity/      # instancias de ECS: launch template, ASG y capacity provider
│       └── service/       # servicio de ECS: task definition, servicio y autoscaling
└── apps/
    ├── ledger/            # backend en Go: binarios api y archivador, imagen con su Dockerfile
    ├── web/               # frontend estático (lo sube modules/website)
    ├── callback/          # Lambda del callback de Cognito (JavaScript, sin dependencias ni build)
    └── webhook/           # Lambda que entrega los avisos del webhook desde la cola SQS
```

Arquitectura y decisiones de diseño: [`ARCHITECTURE.md`](ARCHITECTURE.md).
