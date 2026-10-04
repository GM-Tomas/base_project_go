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

# AUTH_DEV_USER_ID apaga la autenticación: todo request (con o sin token) actúa como ese usuario.
$env:AUTH_DEV_USER_ID = "00000000-0000-0000-0000-000000000001"
go run ./cmd/api
```
Swagger y el "Skip login (dev)" del frontend funcionan sin token. Sin `AUTH_DEV_USER_ID` la API exige JWT reales de Supabase.
En Vercel se ignora siempre (la variable `VERCEL` la desactiva).
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
valida contra el JWKS público. Con el secreto HS256 legacy el JWKS está vacío y todo request da `401`.

Los índices de MongoDB (plataforma única por usuario, un snapshot por segundo) se crean solos al arrancar.

---

## 🗄️ Persistencia: MongoDB

El adaptador `internal/infrastructure/adapter/outbound/persistence/mongo/` implementa los puertos outbound
(`HoldingRepository`, `PlatformRepository`, `SnapshotRepository`, `WealthAggregationPort`) sobre las
colecciones `holdings`, `platforms` y `net_worth_snapshots`. Los montos se guardan como decimales en texto
(escala 2) para no perder precisión.

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
| `GET` | `/swagger`, `/api/v1/openapi.json` | — (documentación) | Basic auth si hay `DOCS_PASSWORD`; en Vercel, `404` sin ella |
| `GET` | `/api/v1/wealth/summary` | Dashboard, Platforms (net worth, YTD, liquidez, desgloses) | Sí |
| `GET` | `/api/v1/holdings` | Assets, drill-down de Platforms, contador | Sí |
| `POST` | `/api/v1/holdings` | Modal "Add an asset" (crea la plataforma si es nueva) | Sí |
| `DELETE` | `/api/v1/holdings/{id}` | Assets (borra también la plataforma si quedó vacía) | Sí |
| `GET` | `/api/v1/platforms` | Selector de plataforma del modal, contador "Accounts" | Sí |
| `GET` | `/api/v1/asset-classes` | Selector de clase y filtros de Assets | Sí |
| `GET` | `/api/v1/wealth/estimate?contribution&yieldPct&years` | Estimate (hitos fijos 150k/250k) | Sí |
| `GET` | `/api/v1/wealth/snapshots` | History | Sí |
| `POST` | `/api/v1/wealth/snapshots` | History → "Save a snapshot" (`409` si ya hay uno en ese segundo) | Sí |
