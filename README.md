# BASE Wealth Management - Backend API (Go)

API REST moderna, eficiente y testeable construida en **Go 1.25** siguiendo los principios de **Arquitectura Hexagonal (Ports & Adapters)**, **Domain-Driven Design (DDD)** y las mejores prácticas de la industria.

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
│   │   └── outbound/                     # Adaptadores de Persistencia (PostgreSQL con pgx, MongoDB)
│   ├── app/                              # Bootstrapper compartido (local & serverless Vercel)
│   └── config/                           # Configuración y variables de entorno
└── errors/                               # Errores tipados de la aplicación
```

---

## 🚀 Comandos Rápidos

### 1. Ejecutar la API en modo Desarrollo
```powershell
# Iniciar Postgres con Docker Compose
docker compose up -d

# Ejecutar la API
go run ./cmd/api
```
- API REST: `http://localhost:8080`
- **Swagger UI Interactivo**: `http://localhost:8080/swagger` o `http://localhost:8080/docs`
- **Especificación OpenAPI 3.1 JSON**: `http://localhost:8080/api/v1/openapi.json`

### 2. Ejecutar la Suite de Tests Automatizados
```powershell
go test -v ./...
```

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

---

## ☁️ Despliegue en Vercel (Serverless Go)

- **`api/index.go`**: entrypoint serverless. Construye la app una vez por instancia caliente; si la inicialización
  falla (p. ej. DB inalcanzable en el cold start) responde `503` y reintenta en el siguiente request.
- **`vercel.json`**: reescribe todas las rutas hacia la función.

### Variables de entorno (Project Settings → Environment Variables)

| Variable | Requerida | Valor |
|---|---|---|
| `SUPABASE_URL` | Sí | `https://<ref>.supabase.co` — el **mismo** proyecto que usa el frontend. De acá se derivan JWKS e issuer. |
| `DATABASE_URL` (o `SUPABASE_DB_URL`) | Sí (Postgres) | Connection string del **Session pooler** de Supabase (`postgres://postgres.<ref>:<pwd>@aws-0-<region>.pooler.supabase.com:5432/postgres?sslmode=require`). Sin `sslmode=require` pgx usa `prefer`, que acepta caer a texto plano. La conexión directa es solo IPv6 (Vercel no la alcanza); el Session pooler además es compatible con los prepared statements que usa pgx por defecto. |
| `MONGODB_URI` | Sí (Mongo) | Si está presente se usa MongoDB en lugar de Postgres (`DB_TYPE` lo fuerza explícitamente). |
| `FRONTEND_ORIGIN` | Recomendada en producción | Orígenes CORS separados por coma. Default: `localhost:3000` y `https://*.vercel.app` (cualquier sitio de Vercel). En producción: la URL exacta del frontend. |

El proyecto de Supabase debe firmar los JWT con **claves asimétricas** (Authentication → JWT Keys): la API
valida contra el JWKS público. Con el secreto HS256 legacy el JWKS está vacío y todo request da `401`.

Antes del primer deploy, aplicar [`db/migrations/000002_wealth_tables.up.sql`](db/migrations/000002_wealth_tables.up.sql)
en el SQL Editor de Supabase.

---

## 🗄️ Persistencia: PostgreSQL o MongoDB

Ambos adaptadores implementan los mismos puertos outbound (`internal/domain/port/outbound/`):
`HoldingRepository`, `PlatformRepository`, `SnapshotRepository` y `WealthAggregationPort`. La elección es
solo de configuración (ver tabla anterior); dominio y handlers no cambian.

---

## 🔐 Autenticación y Seguridad

Todo endpoint bajo `/api/v1/**` excepto `/api/v1/health` exige un token JWT Bearer emitido por Supabase Auth:

```http
Authorization: Bearer <session.access_token>
```

- La validación se realiza contra el endpoint JWKS del proyecto de Supabase (`SUPABASE_URL`), verificando `issuer`, expiración y audiencia `authenticated`.
- La identidad del usuario (`sub` del JWT) es la **única fuente** del `userId` en el backend. Ningún endpoint acepta un identificador de usuario en body, path o query (aislamiento estricto multi-tenant).
- Respuestas de error estructuradas conforme a la especificación **RFC 9457 / RFC 7807** (`application/problem+json`) con identificador de traza `traceId` / `X-Request-Id`.

---

## 🌐 Catálogo de Endpoints REST

| Método | Endpoint | Lo usa (frontend) | Auth |
|---|---|---|---|
| `GET` | `/api/v1/health` | — (monitoreo) | No |
| `GET` | `/swagger`, `/api/v1/openapi.json` | — (documentación) | No |
| `GET` | `/api/v1/wealth/summary` | Dashboard, Platforms (net worth, YTD, liquidez, desgloses) | Sí |
| `GET` | `/api/v1/holdings` | Assets, drill-down de Platforms, contador | Sí |
| `POST` | `/api/v1/holdings` | Modal "Add an asset" (crea la plataforma si es nueva) | Sí |
| `DELETE` | `/api/v1/holdings/{id}` | Assets (borra también la plataforma si quedó vacía) | Sí |
| `GET` | `/api/v1/platforms` | Selector de plataforma del modal, contador "Accounts" | Sí |
| `GET` | `/api/v1/asset-classes` | Selector de clase y filtros de Assets | Sí |
| `GET` | `/api/v1/wealth/estimate?contribution&yieldPct&years` | Estimate (hitos fijos 150k/250k) | Sí |
| `GET` | `/api/v1/wealth/snapshots` | History | Sí |
| `POST` | `/api/v1/wealth/snapshots` | History → "Save a snapshot" (`409` si ya hay uno en ese segundo) | Sí |
