# BASE Wealth Management - Backend API (Go)

API REST moderna, eficiente y testeable construida en **Go 1.26** siguiendo los principios de **Arquitectura Hexagonal (Ports & Adapters)**, **Domain-Driven Design (DDD)** y las mejores prácticas de la industria.

El frontend (Next.js) vive en el repositorio: [GM-Tomas/base_project_fe](https://github.com/GM-Tomas/base_project_fe).

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
# MongoDB local (mongodb://localhost:27017, el default de MONGODB_URI)
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

Los tests de Mongo (`persistence/mongo`, `app`) se saltan si no hay `MONGO_TEST_URI`. Para correrlos y medir coverage (mínimo 85%):
```powershell
docker run -d --rm --name base-wealth-test-mongo -p 27018:27017 mongo:7
$env:MONGO_TEST_URI = "mongodb://localhost:27018"; go test ./... -coverprofile=coverage.out; go tool cover -func=coverage.out | Select-Object -Last 1
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

Supabase se usa **solo como proveedor de identidad** (login). El proyecto debe firmar los JWT con **claves asimétricas** (Authentication → JWT Keys): la API
valida contra el JWKS público. Con el secreto HS256 legacy el JWKS está vacío y todo request con token da `503`.

Los índices de MongoDB (un snapshot por segundo y por usuario, listado de holdings por usuario) se crean solos al arrancar.
Al actualizar una base existente, dos índices de `holdings` que crearon versiones anteriores ya no los usa ninguna
consulta y se pueden borrar: `db.holdings.dropIndex("user_id_1_platform_name_1")` y, si algún build previo de esta
versión llegó a crearlo, `db.holdings.dropIndex("user_id_1_created_at_1")`.

---

## 🗄️ Persistencia: MongoDB

El adaptador `internal/infrastructure/adapter/outbound/persistence/mongo/` implementa los puertos outbound
(`HoldingRepository`, `PlatformRepository`, `SnapshotRepository`, `WealthAggregationPort`) sobre las
colecciones `holdings` y `net_worth_snapshots`. Los montos se guardan como decimales en texto
(escala 2) para no perder precisión.

Las plataformas no se guardan aparte: son los nombres que usan los holdings del usuario, sin distinguir mayúsculas
("Binance" y "binance" son la misma, escrita como en su holding más antiguo, y así la muestran todos los endpoints).
Aparecen con el primer holding y desaparecen con el último. La colección `platforms` de versiones anteriores solo se lee,
para conservar el tipo (Broker, Wallet...) que se eligió entonces para cada nombre, también si esa plataforma se vuelve a
usar más adelante; nada escribe en ella. Las demás son de tipo `Other`.

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

- **Cómo se aísla:** cada documento de MongoDB (`holdings`, `net_worth_snapshots`) guarda el `user_id` (el `sub` del JWT)
  y **toda** lectura, escritura y borrado filtra por él, incluidos las plataformas, los agregados del resumen y la proyección.
  Dos cuentas pueden tener una plataforma "Binance" o un snapshot en el mismo segundo sin chocar.
- **Recursos ajenos:** borrar un holding de otra cuenta (aunque se conozca su id) responde `404`, igual que uno inexistente,
  y un upsert con un id ajeno falla en vez de sobrescribirlo.
- **Agregar usuarios:** en Supabase, *Authentication → Users → Add user* (email + contraseña). Los sign-ups públicos están
  desactivados a propósito: solo entra quien vos des de alta. No hay que tocar nada en la API: la primera vez que un usuario
  nuevo inicia sesión ve su dashboard vacío.
- **Cuotas por usuario** (todas las cuentas comparten la base): hasta **1000 holdings** y **5000 snapshots** por cuenta.
  Al superarlas la API responde `409` con `type` `.../limit-exceeded`.
- **Borrar los datos de una cuenta** (p. ej. al eliminar al usuario en Supabase): sus datos en MongoDB no se borran solos.
  ```js
  // mongosh, con el id (UUID) del usuario de Supabase
  const uid = "<uuid>";
  // ("platforms" solo existe en bases de versiones anteriores)
  ["holdings", "net_worth_snapshots", "platforms"].forEach(c => db.getCollection(c).deleteMany({ user_id: uid }));
  ```
- **Tests:** `internal/infrastructure/app/multiuser_test.go` levanta la API completa (router, auth, servicios y MongoDB real)
  con un JWKS de prueba y verifica con dos usuarios que ninguno ve ni modifica holdings, plataformas, clases de activo,
  snapshots, resumen o proyección del otro, que un `userId` en el body o la query se ignora, y que en modo dev las cuentas
  reales siguen separadas.

---

## 🌐 Catálogo de Endpoints REST

| Método | Endpoint | Lo usa (frontend) | Auth |
|---|---|---|---|
| `GET` | `/api/v1/health` | — (monitoreo) | No |
| `GET` | `/swagger`, `/api/v1/openapi.json` | — (documentación) | Basic auth si hay `DOCS_PASSWORD`; en Vercel, `404` sin ella |
| `GET` | `/api/v1/wealth/summary` | Dashboard, Platforms (net worth, YTD, liquidez, desgloses) | Sí |
| `GET` | `/api/v1/holdings` | Assets, drill-down de Platforms, contador | Sí |
| `POST` | `/api/v1/holdings` | Modal "Add an asset" (crea la plataforma si es nueva; `409` al superar 1000 holdings) | Sí |
| `DELETE` | `/api/v1/holdings/{id}` | Assets (borra también la plataforma si quedó vacía) | Sí |
| `GET` | `/api/v1/platforms` | Selector de plataforma del modal, contador "Accounts" | Sí |
| `GET` | `/api/v1/asset-classes` | Selector de clase y filtros de Assets | Sí |
| `GET` | `/api/v1/wealth/estimate?contribution&yieldPct&years` | Estimate (hitos fijos 150k/250k) | Sí |
| `GET` | `/api/v1/wealth/snapshots` | History | Sí |
| `POST` | `/api/v1/wealth/snapshots` | History → "Save a snapshot" (`409` si ya hay uno en ese segundo o al superar 5000) | Sí |
