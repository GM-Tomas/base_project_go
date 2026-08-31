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
La API estará disponible en `http://localhost:8080`.

### 2. Ejecutar la Suite de Tests Automatizados
```powershell
go test -v -race ./...
```

### 3. Compilar el Binario
```powershell
go build -o bin/api.exe ./cmd/api
```

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

---

## 💾 Persistencia y Migraciones

Las migraciones SQL están en `db/migrations/` con soporte para PostgreSQL nativo y Supabase:
- `000002_wealth_tables.up.sql`: Crea tablas `platforms`, `holdings` y `net_worth_snapshots` con Row Level Security (RLS) y restricciones de integridad.
- `000003_drop_kv_store.up.sql`: Limpieza de tablas previas.
- `000004_tasks_indexes.up.sql`: Índices optimizados.

Para cargar datos de ejemplo en desarrollo:
```powershell
psql "$SUPABASE_DB_URL" -v dev_user_id="'<tu-user-id-uuid>'" -f db/seed/seed-dev.sql
```

---

## 🧪 Pruebas Automatizadas

La suite de tests cubre:
- **Dominio**: Operaciones monetarias exactas con `shopspring/decimal`, fórmulas de interés compuesto (golden test cases con tolerancia `0.01`), desgloses de liquidez, proyecciones e hitos.
- **Servicios de Aplicación**: Lógica de orquestación, validación de reglas de negocio y manejo de errores.
- **Handlers HTTP & Middlewares**: Códigos de estado (`200`, `201`, `204`, `400`, `401`, `404`, `409`), cabeceras (`Location`, `Cache-Control`, `X-Request-Id`), y respuestas `application/problem+json`.
