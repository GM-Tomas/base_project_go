# BASE Wealth Management - Backend API (Go)

API REST moderna, eficiente y testeable construida en **Go 1.26** siguiendo los principios de **Arquitectura Hexagonal (Ports & Adapters)**, **Domain-Driven Design (DDD)** y las mejores prácticas de la industria.

El frontend (Next.js) vive en el repositorio: [GM-Tomas/base_project_fe](https://github.com/GM-Tomas/base_project_fe).

Las **specs** de producto y de API (desarrollo guiado por specs, fase por fase) también viven ahí:
[`base_project_fe/specs`](https://github.com/GM-Tomas/base_project_fe/tree/main/specs). Cada cambio de este
repo sigue la spec de su fase.

---

## 📋 Arquitectura del Proyecto (Hexagonal)

El proyecto sigue una separación estricta de responsabilidades en tres capas fundamentales:

```
internal/
├── domain/                               # NÚCLEO DE DOMINIO (Independiente de frameworks/DB)
│   ├── model/                            # Value Objects y Entidades (Money, Holding, Platform, Snapshot, etc.)
│   ├── service/                          # Lógica financiera (CompoundInterestCalculator, LiquidityPolicy, etc.)
│   └── port/                             # Interfaces de puertos
│       ├── inbound/                      # Use Cases y Comandos (HoldingUseCase, PlatformUseCase, etc.)
│       └── outbound/                     # Contratos de repositorios (HoldingRepository, SnapshotRepository, etc.)
├── application/                          # CAPA DE APLICACIÓN (Casos de uso y DTOs)
│   ├── dto/                              # DTOs de entrada y salida
│   └── service/                          # Orquestación de servicios (HoldingService, WealthQueryService, etc.)
├── infrastructure/                       # CAPA DE INFRAESTRUCTURA (Detalles de IO y adaptadores)
│   ├── adapter/
│   │   ├── inbound/                      # Adaptadores HTTP (Chi Router, Handlers y Middlewares)
│   │   └── outbound/                     # Adaptador de Persistencia (MongoDB)
│   ├── app/                              # Bootstrapper compartido (local & serverless Vercel)
│   └── config/                           # Configuración y variables de entorno
└── errors/                               # Errores tipados de la aplicación
```

---

## 🚀 Comandos Rápidos

### 1. Ejecutar la API 100% local (sin Atlas ni Supabase)
```powershell
# MongoDB local como replica set de un nodo (mongodb://localhost:27017/?directConnection=true, el default de
# MONGODB_URI). Las transacciones lo necesitan; el healthcheck lo inicializa solo.
docker compose up -d

# AUTH_DEV_USER_ID: los requests SIN token actúan como ese usuario. Los que traen token se verifican igual que en producción.
$env:AUTH_DEV_USER_ID = "00000000-0000-0000-0000-000000000001"
go run ./cmd/api
```
Swagger y el "Skip login (dev)" del frontend funcionan sin token (como ese usuario de dev). Si iniciás sesión en el frontend con
cuentas reales de Supabase, cada una ve sus propios datos también en local; un token inválido da `401`, nunca "cae" al usuario de dev.
Sin `AUTH_DEV_USER_ID` la API exige JWT reales de Supabase. En Vercel se ignora siempre (la variable `VERCEL` la desactiva).
Si antes de este cambio cargaste datos en local con la sesión iniciada, quedaron guardados bajo el usuario de dev: se ven con
"Skip login (dev)".
- API REST: `http://localhost:8080`
- **Swagger UI Interactivo**: `http://localhost:8080/swagger` o `http://localhost:8080/docs`
- **Especificación OpenAPI 3.1 JSON**: `http://localhost:8080/api/v1/openapi.json`

### 2. Ejecutar la Suite de Tests Automatizados
```powershell
go test -v ./...
```

Los tests de Mongo (`persistence/mongo`, `app`) se saltan si no hay `MONGO_TEST_URI`. Necesitan un replica set (las
transacciones lo requieren). Para correrlos y medir coverage (mínimo 85%), con los datos en RAM (`--tmpfs`: cada test
crea y borra su base, y en disco eso es casi todo su tiempo) y el puerto en IPv4 (con Podman/WSL `localhost` puede
resolver a `::1` y cortar las conexiones):
```powershell
docker run -d --rm --name base-wealth-test-mongo --tmpfs /data/db -p 127.0.0.1:27018:27017 mongo:7 --replSet rs0 --bind_ip_all
docker exec base-wealth-test-mongo mongosh --quiet --eval "rs.initiate({_id:'rs0',members:[{_id:0,host:'localhost:27017'}]})"
$env:MONGO_TEST_URI = "mongodb://127.0.0.1:27018/?directConnection=true"; go test ./... -coverprofile=coverage.out; go tool cover -func=coverage.out | Select-Object -Last 1
docker stop base-wealth-test-mongo
```
(`make test-coverage` hace lo mismo y falla por debajo del 85%.)

### 3. Compilar el Binario
```powershell
go build -o bin/api.exe ./cmd/api
```

---

## 📖 Contrato API

El contrato es exactamente lo que consume el frontend (`base_project_fe/src/lib/api.ts`), ni más ni menos.
La especificación OpenAPI 3.1 vive embebida en el binario
([`internal/infrastructure/adapter/inbound/http/openapi.json`](internal/infrastructure/adapter/inbound/http/openapi.json))
y se sirve en `/api/v1/openapi.json` y `/swagger`.

En Vercel la documentación no es pública: pide basic auth (usuario `docs`, contraseña = `DOCS_PASSWORD`) y, si esa
variable está vacía o no existe, `/docs`, `/swagger` y `openapi.json` dan `404`. Las variables se leen al arrancar:
después de cambiar `DOCS_PASSWORD` hay que redeployar. En local sigue abierta salvo que se defina `DOCS_PASSWORD`.

---

## ☁️ Despliegue en Vercel (Go Framework Preset)

- **`vercel.json`** fija `"framework": "go"`: Vercel compila y corre `cmd/api/main.go` como servidor HTTP en `$PORT`,
  con las rutas tal cual (sin rewrites). Si MongoDB no responde al arrancar, el proceso termina y Vercel lo reinicia.
- Las variables vienen de Vercel; no hay `.env` en producción. `MONGODB_URI` la inyecta la integración de Atlas.

### Variables de entorno (Project Settings → Environment Variables)

| Variable | Requerida | Valor |
|---|---|---|
| `MONGODB_URI` | Sí | Connection string de MongoDB (Atlas: `mongodb+srv://...`). En Atlas, habilitar el acceso desde Vercel en *Network Access* (`0.0.0.0/0`: Vercel no tiene IPs fijas). Default local: `mongodb://localhost:27017`. |
| `MONGODB_DATABASE` | No | Default `base_wealth`. |
| `SUPABASE_URL` | Sí | `https://<ref>.supabase.co`: el **mismo** proyecto que usa el frontend para el login. Solo se usa para validar los JWT (JWKS e issuer). |
| `DOCS_PASSWORD` | No | Protege `/docs`, `/swagger` y `openapi.json` con basic auth (usuario `docs`). En Vercel, sin ella esas rutas dan `404`. |
| `FRONTEND_ORIGIN` | Opcional | Orígenes CORS separados por coma. Default: `localhost:3000` y `https://base-project-fe.vercel.app`. Nunca `https://*.vercel.app`: cualquiera puede desplegar ahí. |

**Deploys de preview** (cada rama o PR: en Vercel, `VERCEL=1` con `VERCEL_TARGET_ENV=preview`, o `VERCEL_ENV=preview` si
falta): la API **nunca se conecta a MongoDB**, aunque
`MONGODB_URI` le llegue (hoy en Vercel apunta a Preview y Production; se puede destildar Preview). Los endpoints de
datos responden `503` (`.../preview-without-data`); `GET /api/v1/health` sigue respondiendo. Los previews del frontend
corren con datos de demo en el navegador, así que no la necesitan. Solo producción usa la base real (un entorno custom
de Vercel, como "staging", no cuenta como preview: Vercel pone su nombre en `VERCEL_TARGET_ENV`). Como cada deploy se
decide por sus propias variables, producción tiene que ser un deploy de producción (push a `main` o redeploy a
Production): un deploy de preview apuntado a producción sin rebuild (el endpoint REST de promote de Vercel no
reconstruye) seguiría respondiendo `503` hasta el próximo build de producción.

Supabase se usa **solo como proveedor de identidad** (login). El proyecto debe firmar los JWT con **claves asimétricas** (Authentication → JWT Keys): la API
valida contra el JWKS público. Con el secreto HS256 legacy el JWKS está vacío y todo request con token da `503`.

Los índices de MongoDB (un snapshot por segundo y por usuario, listados de holdings, deudas y actividad por usuario) se
crean solos al arrancar.
Al actualizar una base existente, dos índices de `holdings` que crearon versiones anteriores ya no los usa ninguna
consulta y se pueden borrar: `db.holdings.dropIndex("user_id_1_platform_name_1")` y, si algún build previo de esta
versión llegó a crearlo, `db.holdings.dropIndex("user_id_1_created_at_1")`.

---

## 🗄️ Persistencia: MongoDB

El adaptador `internal/infrastructure/adapter/outbound/persistence/mongo/` implementa los puertos outbound
(`HoldingRepository`, `DebtRepository`, `PlatformRepository`, `SnapshotRepository`, `MovementRepository`,
`QuotaRepository`, `PreferencesRepository`, `AssetClassSettingsRepository`, `PlatformSettingsRepository`,
`WealthAggregationPort`, `TransactionManager`) sobre las colecciones `holdings`, `debts` (lo que se debe),
`net_worth_snapshots`, `movements` (la actividad: cada cambio de valor de un holding o del saldo de una deuda), `quotas`
(contadores por usuario), `preferences` (un documento por usuario, con su id como `_id`: cómo dejó Estimate, el checkpoint mensual automático, la vista inicial y el período de History),
`asset_class_settings` y `platform_settings` (cómo configuró sus clases y sus plataformas, ver abajo). Los montos y las tasas se guardan como decimales en texto (escala 2) para no perder
precisión.

**Retorno esperado.** Cada holding puede decir cuánto rinde por año (`expected_return_pct`, −100 a 100; ausente si no
se sabe). El del portfolio es el promedio ponderado por valor (los que no tienen cuentan como 0 %), con la cobertura (%
del valor con retorno cargado) y lo que rendiría en dólares: `expectedReturn` del resumen. La proyección crece a ese
retorno salvo que se le pase `yieldPct`, y simula mes a mes (aporte que sube cada año, inflación para ver los valores en
dólares de hoy).

**Historial por períodos.** `GET /movements/summary` suma los movimientos de un período por categoría con un
`$group` de MongoDB (no lee los movimientos uno por uno) y el dominio (`service.MovementsEffect`) dice qué le hicieron
al patrimonio: rendimiento (ganancias − pérdidas − comisiones − intereses de deudas), ahorro (depósitos − retiros ±
pagos y cargos de deudas con plata de afuera), altas y bajas, y correcciones. Las transferencias y los pagos de deuda
desde un asset no lo cambian (salvo la comisión): solo se cuentan. Los snapshots pueden cargarse a mano con fecha
pasada (`source: MANUAL`, con nota) para que la historia empiece antes de usar BASE.

**Patrimonio neto = assets − deudas.** El resumen, los snapshots y los hitos de la proyección usan el neto, que puede ser
negativo. Cada snapshot guarda también lo que se tenía (`assets_usd`) y lo que se debía (`debts_usd`); los anteriores a
las deudas no los tienen y se leen como assets = total y deudas = 0. La proyección parte del portafolio (los assets) y
amortiza cada deuda aparte, con su cuota y su tasa.

**Transacciones.** Cada cambio de valor se escribe junto con su movimiento en una transacción (alta, edición y baja
de holdings y deudas, movimientos, pagos de deudas desde un holding, deshacer): todo o nada. MongoDB solo tiene transacciones en un **replica set**: Atlas ya
lo es (incluido el tier gratuito); en local, `compose.yaml` levanta uno de un nodo. Contra un `mongod` standalone la
API arranca y lee igual, avisa en el log, y esas escrituras responden `503` (`.../transactions-unavailable`). Un
volumen creado antes con el compose standalone sirve tal cual: el contenedor nuevo lo reinicia como replica set.

Las plataformas no se guardan aparte: son los nombres que usan los holdings del usuario, sin distinguir mayúsculas
("Binance" y "binance" son la misma, escrita como en su holding más antiguo, y así la muestran todos los endpoints).
Aparecen con el primer holding y desaparecen con el último. La colección `platforms` de versiones anteriores solo se lee,
para conservar el tipo (Broker, Wallet...) que se eligió entonces para cada nombre, también si esa plataforma se vuelve a
usar más adelante; nada escribe en ella. Las demás son de tipo `Other`.

**Clases y plataformas configurables.** Lo que el usuario cambia se guarda como una "excepción" sobre lo de por defecto,
un documento por clase o por plataforma (`_id` = id del usuario + id de la clase o plataforma, así que hay uno solo por
cada una): no hace falta sembrar nada por cuenta y cambiar la configuración del servidor sigue alcanzando a quien no la
tocó.

- `asset_class_settings`: `name`, `color`, `liquid`, `expected_return_pct`, `hidden`. Las clases visibles son las por
  defecto (`DEFAULT_ASSET_CLASSES`) menos las ocultas, más las creadas, más las que usan los holdings. Una clase creada
  existe por su documento (aparece sin holdings); una por defecto borrada queda `hidden` (no vuelve, salvo que se cree de
  nuevo o un holding la use). `liquid` reemplaza a `WEALTH_LIQUID_ASSET_CLASSES` para esa clase y `expected_return_pct`
  es el retorno con el que cuentan sus holdings sin retorno propio (`effectiveReturnPct`, el retorno del portfolio y la
  proyección).
- `platform_settings`: `key` (el nombre sin mayúsculas ni formas Unicode, lo que agrupa los holdings en una plataforma),
  `type`, `avatar_text` (1 o 2 caracteres; un emoji cuenta como uno), `color`. Se conserva si la plataforma se queda sin
  holdings y vuelve a usarse.
- **Renombrar o fusionar** una clase o una plataforma actualiza sus holdings y mueve o fusiona su configuración en una
  transacción (la de destino conserva la suya). Los movimientos conservan los nombres que tenían: son historia. Renombrar
  no cambia el `updatedAt` de los holdings (no es una edición del asset).

---

## 🔐 Autenticación y Seguridad

Todo endpoint bajo `/api/v1/**` excepto `/api/v1/health` exige un token JWT Bearer emitido por Supabase Auth:

```http
Authorization: Bearer <session.access_token>
```

- La validación se realiza contra el endpoint JWKS del proyecto de Supabase (`SUPABASE_URL`), verificando firma, `issuer`, expiración y audiencia `authenticated`. Las sesiones anónimas se rechazan.
- La API guarda el JWKS en memoria y lo vuelve a pedir cuando tiene más de 10 minutos, lo que Supabase recomienda
  (su edge lo cachea otros 10). Si Supabase no responde, sigue con las claves que tiene hasta que cumplen 20 minutos;
  pasado eso, o si todavía no tiene ninguna, responde `503`, no `401`: el token puede ser válido y el frontend solo
  cierra la sesión ante un `401`.
- Un token firmado con una clave que la API todavía no conoce da `401`. Para **rotar claves** en Supabase
  (Authentication → JWT Keys): crear la nueva como *standby*, esperar al menos 20 minutos (así toda instancia de la
  API ya la tiene) y recién entonces rotar. Revocar una clave también tarda hasta 20 minutos en llegar a la API
  (30 si Supabase no respondía justo al renovar las claves).
- La identidad del usuario (`sub` del JWT) es la **única fuente** del `userId` en el backend. Ningún endpoint acepta un identificador de usuario en body, path o query (aislamiento estricto multi-tenant; si se envía, se ignora).
- Respuestas de error estructuradas conforme a la especificación **RFC 9457 / RFC 7807** (`application/problem+json`) con identificador de traza `traceId` / `X-Request-Id`.

---

## 👥 Multi-usuario: cada cuenta ve solo sus datos

La app es multi-usuario: cada persona inicia sesión con su propia cuenta de Supabase y ve y modifica **solo sus datos**.

- **Cómo se aísla:** cada documento de MongoDB (`holdings`, `debts`, `net_worth_snapshots`, `movements`,
  `asset_class_settings`, `platform_settings`) guarda el
  `user_id` (el `sub` del JWT; en `preferences` es el `_id`)
  y **toda** lectura, escritura y borrado filtra por él, incluidos las plataformas, los agregados del resumen y la proyección.
  Dos cuentas pueden tener una plataforma "Binance" o un snapshot en el mismo segundo sin chocar.
- **Recursos ajenos:** borrar un holding de otra cuenta (aunque se conozca su id) responde `404`, igual que uno inexistente,
  y un upsert con un id ajeno falla en vez de sobrescribirlo.
- **Agregar usuarios:** en Supabase, *Authentication → Users → Add user* (email + contraseña). Los sign-ups públicos están
  desactivados a propósito: solo entra quien vos des de alta. No hay que tocar nada en la API: la primera vez que un usuario
  nuevo inicia sesión ve su dashboard vacío.
- **Cuotas por usuario** (todas las cuentas comparten la base): hasta **1000 holdings**, **200 deudas**, **5000
  snapshots**, **20000 movimientos**, **100 clases** creadas o configuradas y **1000 plataformas** personalizadas por cuenta. Al superarlas la API responde `409` con `type` `.../limit-exceeded`.
  Los movimientos se cuentan en `quotas`, dentro de la misma transacción: dos pedidos simultáneos del mismo usuario se
  serializan, así que las cuotas de holdings, deudas y movimientos son exactas (borrar un holding o una deuda siempre
  funciona: su `CLOSING` no cuenta).
- **Borrar los datos de una cuenta** (p. ej. al eliminar al usuario en Supabase): sus datos en MongoDB no se borran solos.
  ```js
  // mongosh, con el id (UUID) del usuario de Supabase
  const uid = "<uuid>";
  // ("platforms" solo existe en bases de versiones anteriores)
  ["holdings", "debts", "net_worth_snapshots", "movements", "platforms", "asset_class_settings", "platform_settings"]
    .forEach(c => db.getCollection(c).deleteMany({ user_id: uid }));
  db.quotas.deleteOne({ _id: uid });
  db.preferences.deleteOne({ _id: uid });
  ```
- **Tests:** `internal/infrastructure/app/multiuser_test.go` levanta la API completa (router, auth, servicios y MongoDB real)
  con un JWKS de prueba y verifica con dos usuarios que ninguno ve ni modifica holdings, deudas, movimientos,
  plataformas, clases de activo (ni su configuración ni sus renombres), snapshots, resumen, proyección, retornos
  esperados o preferencias del otro, que un `userId` en el body o la query se ignora, y que en modo dev las cuentas
  reales siguen separadas.

---

## 🌐 Catálogo de Endpoints REST

| Método | Endpoint | Lo usa (frontend) | Auth |
|---|---|---|---|
| `GET` | `/api/v1/health` | — (monitoreo) | No |
| `GET` | `/swagger`, `/api/v1/openapi.json` | — (documentación) | Basic auth si hay `DOCS_PASSWORD`; en Vercel, `404` sin ella |
| `GET` | `/api/v1/wealth/summary` | Dashboard, Platforms (net worth = assets − deudas, YTD, liquidez, desgloses) | Sí |
| `GET` | `/api/v1/holdings` | Assets, drill-down de Platforms, contador | Sí |
| `POST` | `/api/v1/holdings` | Modal "Add an asset" (crea la plataforma si es nueva; registra su `OPENING`; `409` al superar 1000 holdings) | Sí |
| `PATCH` | `/api/v1/holdings/{id}` | Edit asset (solo cambia lo enviado; un valor nuevo queda registrado según `valueChangeReason`; `expectedReturnPct`, `null` lo borra; `404` si no existe o es ajeno) | Sí |
| `PUT` | `/api/v1/holdings/expected-returns` | "Set expected returns" (varios retornos a la vez, todo o nada; `404` si algún id es ajeno) | Sí |
| `DELETE` | `/api/v1/holdings/{id}` | Assets (registra su `CLOSING`; borra también la plataforma si quedó vacía) | Sí |
| `GET` | `/api/v1/debts` | Debts (por saldo; cada una con su `payoff`: cuándo se cancela con su cuota) | Sí |
| `POST` | `/api/v1/debts` | "Add a debt" (registra su `OPENING`; `409` al superar 200 deudas) | Sí |
| `PATCH` | `/api/v1/debts/{id}` | Edit debt (*merge patch*, `null` borra los opcionales; un saldo nuevo queda registrado según `balanceChangeReason`) | Sí |
| `DELETE` | `/api/v1/debts/{id}` | Debts (registra su `CLOSING`) | Sí |
| `GET` | `/api/v1/movements` | Activity y detalle de un asset o una deuda (`holdingId`, `debtId`, `kind`, `from`, `to`, `limit`, `cursor`) | Sí |
| `POST` | `/api/v1/movements` | Gain/loss, deposit/withdrawal, transfer; pago, cargo e interés de una deuda (`409` si un valor o saldo quedaría negativo) | Sí |
| `DELETE` | `/api/v1/movements/{id}` | Undo (revierte como delta; `409` si no se puede) | Sí |
| `GET` | `/api/v1/movements/summary?from&to` | History → "Why it changed" (lo que suman los movimientos del período por categoría y su efecto en el patrimonio) | Sí |
| `GET` | `/api/v1/platforms` | Selector de plataforma del modal, contador "Accounts", Settings (con `id`, miniatura, color, holdings y valor) | Sí |
| `PATCH` | `/api/v1/platforms/{id}` | Settings → Customize (miniatura, color, tipo; `name` renombra en todos sus holdings; `409 platform-exists` salvo `mergeIfExists`) | Sí |
| `GET` | `/api/v1/asset-classes` | Selector de clase y filtros de Assets, Settings (`classes`: color, liquidez, retorno por defecto, holdings y valor) | Sí |
| `POST` | `/api/v1/asset-classes` | Settings → New class (`409 class-exists`; hasta 100) | Sí |
| `PATCH` | `/api/v1/asset-classes/{id}` | Settings → Edit class (`name` renombra en todos sus holdings; `409 class-exists` salvo `mergeIfExists`) | Sí |
| `DELETE` | `/api/v1/asset-classes/{id}?moveTo=` | Settings → Remove class (con holdings hace falta `moveTo`: si no, `409 class-in-use`) | Sí |
| `GET` | `/api/v1/wealth/estimate?contribution&years[&yieldPct&milestones&inflationPct&contributionGrowthPct]` | Estimate (parte de los assets, al retorno esperado del portfolio salvo `yieldPct`; deudas amortizadas aparte; hitos sobre el neto, 150k/250k por defecto) | Sí |
| `GET`, `PUT` | `/api/v1/preferences` | Estimate (cómo lo dejó el usuario), checkpoint mensual automático (`autoSnapshot`), vista inicial (`defaultView`) y período de History (`historyPeriod`), en cualquier dispositivo; el PUT reemplaza el documento | Sí |
| `GET` | `/api/v1/wealth/snapshots` | History | Sí |
| `POST` | `/api/v1/wealth/snapshots` | History → "Save a snapshot" (sin cuerpo) y "Add a past checkpoint" (con `capturedAt` y `totalValueUsd`: `source: MANUAL`); `409` si ya hay uno en ese segundo o al superar 5000 | Sí |
| `DELETE` | `/api/v1/wealth/snapshots/{id}` | History → "Delete checkpoint" (`404` si no existe o es ajeno) | Sí |
