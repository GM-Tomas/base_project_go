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
│   │   └── outbound/                     # Adaptadores de Persistencia (PostgreSQL con pgx)
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

## 📖 Documentación OpenAPI y Guía para Frontend / Agentes de IA

En la carpeta [`docs/`](docs/) encontrarás:
1. **[`docs/openapi.yaml`](docs/openapi.yaml)** y **[`docs/openapi.json`](docs/openapi.json)**: Especificación formal OpenAPI 3.1.0 completa con esquemas de validación, ejemplos y formato de errores RFC 9457.
2. **[`docs/FRONTEND_AGENT_GUIDE.md`](docs/FRONTEND_AGENT_GUIDE.md)**: Guía exhaustiva diseñada para que cualquier Agente de IA o desarrollador frontend conecte el cliente Next.js (`base_project_fe`) sin ambigüedades (incluye reglas de negocio, flujos de autenticación con Supabase, tipos TypeScript y hooks de TanStack Query listos para usar).

---

## ☁️ Despliegue en Vercel (Serverless Go)

El proyecto incluye soporte nativo listo para desplegar en **Vercel Serverless Functions**:
- **`api/index.go`**: Punto de entrada serverless estándar de Vercel (`func Handler(w http.ResponseWriter, r *http.Request)`).
- **`vercel.json`**: Configuración de enrutamiento que redirige todas las rutas hacia la función serverless de Go.

Para desplegar en Vercel con la CLI:
```bash
vercel
```
O simplemente conecta el repositorio de GitHub [GM-Tomas/base_project_go](https://github.com/GM-Tomas/base_project_go) en tu panel de Vercel. Configura las variables de entorno (`SUPABASE_URL`, `DATABASE_URL`, etc.) en el dashboard del proyecto en Vercel.

---

## 🗄️ Persistencia: ¿KVS, PostgreSQL o MongoDB?

### 1. ¿Funciona con un Key-Value Store (KVS)?
**No.** En las primeras plantillas de prueba existía una tabla `kv_store`, pero fue explícitamente eliminada porque una aplicación de gestión patrimonial requiere:
- Claves foráneas e integridad referencial (`holdings` vinculados a `platforms`).
- Precisión decimal fija para saldos financieros (`NUMERIC(20,2)`).
- Row Level Security (RLS) en Supabase para aislamiento multi-tenant estricto.
- Unicidad insensible a mayúsculas (`lower(name)`).
- Agregaciones (`SUM`, `COUNT`, `LEFT JOIN` para incluir plataformas con saldo cero).

### 2. ¿Se puede conectar con MongoDB?
**¡Sí!** Gracias a la **Arquitectura Hexagonal**, el dominio y la lógica de aplicación dependen únicamente de interfaces (puertos outbound en `internal/domain/port/outbound/`):
- `HoldingRepository`
- `PlatformRepository`
- `SnapshotRepository`
- `WealthAggregationPort`

Para usar MongoDB en lugar de PostgreSQL, solo se requiere implementar un adaptador que satisfaga estas 4 interfaces contra colecciones de MongoDB (`holdings`, `platforms`, `snapshots`), sin tocar una sola línea de código del dominio o los controladores.

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

| Método | Endpoint | Descripción | Auth Requerida |
|---|---|---|---|
| `GET` | `/swagger` o `/docs` | Interfaz interactiva de Swagger UI | No |
| `GET` | `/api/v1/openapi.json` | Especificación OpenAPI 3.1 en formato JSON | No |
| `GET` | `/api/v1/health` | Estado operativo del servicio | No |
| `GET` | `/api/v1/holdings` | Lista posiciones del usuario (filtro opcional `assetClass`, `platform`) | Sí |
| `POST` | `/api/v1/holdings` | Crea una posición (alta implícita de plataforma) | Sí |
| `GET` | `/api/v1/holdings/{id}` | Obtiene una posición por su UUID | Sí |
| `PATCH` | `/api/v1/holdings/{id}` | Actualización parcial de una posición | Sí |
| `DELETE` | `/api/v1/holdings/{id}` | Elimina una posición | Sí |
| `GET` | `/api/v1/platforms` | Lista plataformas del usuario (incluyendo saldo 0) | Sí |
| `POST` | `/api/v1/platforms` | Crea una plataforma (`409` si el nombre ya existe) | Sí |
| `PATCH` | `/api/v1/platforms/{name}` | Renombra o recategoriza una plataforma | Sí |
| `DELETE` | `/api/v1/platforms/{name}` | Elimina una plataforma (`409` si tiene posiciones asociadas) | Sí |
| `GET` | `/api/v1/asset-classes` | Clases de activo disponibles (defaults ∪ en uso) | Sí |
| `GET` | `/api/v1/wealth/summary` | Resumen consolidado del patrimonio (USD, ARS, YTD, liquidez, distribución) | Sí |
| `GET` | `/api/v1/wealth/estimate` | Proyección de interés compuesto e hitos (`Cache-Control: private, max-age=30`) | Sí |
| `GET` | `/api/v1/wealth/snapshots` | Serie histórica de snapshots con variación porcentual | Sí |
| `POST` | `/api/v1/wealth/snapshots` | Captura un snapshot instantáneo en el servidor (`409` si ya existe en el segundo) | Sí |
