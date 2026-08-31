# Feature Spec 001 — El backend provee todas las funcionalidades del frontend

| Campo | Valor |
|---|---|
| **Estado** | Draft — pendiente de aprobación |
| **Fecha** | 2026-08-23 |
| **Rama sugerida** | `001-backend-para-frontend` |
| **Autor** | BASE Engineering |
| **Documentos hermanos** | [`plan.md`](plan.md) · [`data-model.md`](data-model.md) · [`contracts/openapi.yaml`](contracts/openapi.yaml) · [`tasks.md`](tasks.md) |

---

## 1. Contexto y problema

El frontend (`frontend/`, Next.js 14 + React + TS) es hoy una **aplicación autónoma**:

- Todo el estado de negocio vive en `WealthContext.tsx` y se persiste en `localStorage`
  (`base_holdings`, `base_platforms`, `base_history`).
- Toda la lógica financiera vive en `lib/calculations.ts` (valor futuro, meses-a-hito, formateo).
- Los datos semilla viven en `lib/constants.ts`.
- El backend (`backend/`, Kotlin + Spring Boot 3, arquitectura hexagonal) **existe pero nadie lo llama**:
  `grep -r "8080\|NEXT_PUBLIC_API" frontend/src` no devuelve nada.

Consecuencias directas:

1. **Los datos no sobreviven al dispositivo.** El usuario se autentica con Google (Supabase Auth),
   pero su patrimonio vive en el `localStorage` del navegador. Cambiar de máquina = empezar de cero.
2. **No hay multi-tenancy.** El backend expone `/api/v1/holdings` global, sin concepto de usuario.
   Dos usuarios verían el mismo patrimonio.
3. **No hay autenticación en el backend.** Cualquiera con la URL puede leer, crear y borrar holdings.
   `POST /api/v1/tasks` acepta `userId` **en el body**: es un bypass de autorización trivial.
4. **Doble fuente de verdad para la matemática financiera.** `calculations.ts` y
   `WealthCalculationService.kt` implementan la misma fórmula con contratos distintos
   (hitos, punto inicial de la serie, formato de fecha). Divergirán.
5. **Datos de dominio hardcodeados en la capa de aplicación.** `PlatformService` y `HistoryService`
   devuelven listas literales: el frontend puede crear plataformas y snapshots, el backend no.

### Objetivo

Que **el backend sea la única fuente de verdad** de todo lo que el frontend necesita —datos,
cálculos y reglas—, expuesto como una API REST versionada, autenticada por usuario, con arquitectura
hexagonal limpia, y que el frontend pase a ser un cliente de esa API.

### No-objetivo

Rediseñar el frontend. La UI y el diseño visual se mantienen; sólo cambia el origen de sus datos.

---

## 2. Inventario: qué necesita el frontend, exactamente

Derivado leyendo cada vista y cada acción del contexto. Esta tabla es el contrato funcional mínimo.

| # | Capacidad del frontend | Origen hoy | Consumidor | Estado backend |
|---|---|---|---|---|
| F1 | Listar holdings | `localStorage` | `AssetsView`, `DashboardView` | ✅ existe, ❌ sin usuario |
| F2 | Crear holding (nombre, clase, plataforma, valor USD) | `addHolding()` | `AddAssetModal` | ✅ existe, ❌ sin usuario |
| F3 | Borrar holding | `deleteHolding()` | `AssetsView` | ✅ existe |
| F4 | Editar holding | — (no hay UI) | — | ✅ existe (`PUT`, semántica incorrecta) |
| F5 | Filtrar holdings por clase de activo | memoria | `AssetsView` | ⚠️ sin query param |
| F6 | Listar holdings de una plataforma | memoria | `PlatformsView` (drill-down) | ⚠️ sin query param |
| F7 | Listar plataformas con su `type` | `localStorage` | `PlatformsView`, `AddAssetModal` | ⚠️ hardcodeado |
| F8 | **Crear plataforma** (alta implícita al crear un holding con plataforma nueva) | `addHolding()` | `AddAssetModal` (`+ Add new platform...`) | ❌ **no existe** |
| F9 | Lista de clases de activo disponibles = defaults ∪ las que están en uso | `availableAssetClasses` | `AddAssetModal`, `AssetsView` | ❌ **no existe** |
| F10 | **Crear clase de activo** (alta implícita) | `addHolding()` | `AddAssetModal` (`+ Add new asset class...`) | ❌ **no existe** |
| F11 | Patrimonio neto total en USD | `reduce` | `DashboardView`, `ProfileModal` | ✅ `/wealth/summary` |
| F12 | Patrimonio neto en ARS (tipo de cambio) | — | (no se muestra aún) | ⚠️ FX hardcodeado a 1050 |
| F13 | Distribución por clase de activo (valor, %, orden desc) | `classDistribution` | donut del `DashboardView` | ✅ `/wealth/summary` |
| F14 | Distribución por plataforma, **incluyendo plataformas con saldo 0** | `platformDistribution` | `DashboardView`, `PlatformsView` | ⚠️ agrupa sólo por holdings → pierde las de saldo 0 |
| F15 | % líquido / % ilíquido | `liquidityPct` | `DashboardView` | ❌ **no existe** |
| F16 | Crecimiento YTD (%) | `ytdGrowthFormatted` | `DashboardView` | ❌ **no existe** |
| F17 | Serie histórica de snapshots | `localStorage` | `HistoryView` | ⚠️ hardcodeada |
| F18 | **Tomar snapshot** del patrimonio actual | `takeSnapshot()` | `HistoryView` | ❌ **no existe** |
| F19 | Variación % de cada snapshot vs el anterior | `snapshotRows` | tabla del `HistoryView` | ❌ **no existe** |
| F20 | Proyección de valor futuro, serie **año 0..N** | `fvYears()` | gráfico del `EstimateView` | ⚠️ backend arranca en año 1 |
| F21 | Hitos $150k y $250k con fecha estimada, dentro del horizonte | `monthsToReach()` | `EstimateView` | ⚠️ backend tiene hitos fijos 100k/250k/500k/1M/2M |
| F22 | Identidad del usuario (nombre, email, avatar) | sesión Supabase | `Sidebar`, `ProfileModal` | n/a — sale del JWT en el cliente |
| F23 | Aislamiento de datos por usuario autenticado | ❌ inexistente | todo | ❌ **no existe** |

**Resumen del gap:** 9 capacidades no existen, 7 existen con contrato incorrecto, y la más
importante —F23, aislamiento por usuario— atraviesa todas las demás.

---

## 3. Alcance

### Dentro de alcance

1. Autenticación y autorización en el backend validando el JWT de Supabase Auth.
2. Modelo de datos multi-usuario persistido en Postgres (Supabase) con migraciones versionadas.
3. API REST completa que cubre F1–F21 (F22 se resuelve en el cliente, ver §6).
4. Refactor de la arquitectura hexagonal existente para eliminar las fugas entre capas (ver `plan.md` §3).
5. Suite de tests y gates de calidad (cobertura, lint estático, tests de arquitectura, contract tests).
6. Reemplazo en el frontend de `localStorage` por un cliente de API, conservando la UI intacta.

### Fuera de alcance (explícito)

| Fuera | Por qué |
|---|---|
| Rediseño visual del frontend | La UI actual es el requisito, no el problema |
| Precios de mercado en vivo (tickers, cotizaciones) | Nadie lo pidió; `value` es un input manual hoy |
| Multi-moneda real (holdings en ARS/EUR nativos) | Hoy todo se guarda en USD; ver *Riesgo R3* |
| Compartir portafolio entre usuarios / cuentas familiares | No hay UI ni requisito |
| Importar/exportar CSV, integraciones con brokers | No hay UI ni requisito |
| Migración automática del `localStorage` existente a la cuenta | Ver §7 *Decisión abierta D2* |
| Event sourcing / CQRS con event store / outbox | YAGNI — ver `plan.md` §4.2 |

---

## 4. Historias de usuario y criterios de aceptación

Formato Given/When/Then. Cada criterio debe ser verificable por un test automatizado.

### HU-01 — Mis datos son míos y sólo míos

> Como usuario autenticado, quiero que mi patrimonio esté asociado a mi cuenta,
> para verlo desde cualquier dispositivo y que nadie más pueda leerlo.

- **CA-01.1** Dado un request sin header `Authorization`, cuando llama a cualquier endpoint bajo
  `/api/v1/**` excepto `/api/v1/health`, entonces responde `401` con `application/problem+json`.
- **CA-01.2** Dado un JWT con firma inválida, expirado, con `iss` distinto al proyecto Supabase
  configurado o con `aud` ≠ `authenticated`, entonces responde `401`.
- **CA-01.3** Dado el usuario A con holdings y el usuario B sin ninguno, cuando B llama a
  `GET /api/v1/holdings`, entonces recibe `200` con lista vacía.
- **CA-01.4** Dado un holding del usuario A, cuando el usuario B llama a
  `GET|PATCH|DELETE /api/v1/holdings/{idDeA}`, entonces responde `404` (**no** `403`: no se
  confirma la existencia de recursos ajenos).
- **CA-01.5** Ningún endpoint acepta un identificador de usuario en body, path o query. La identidad
  se deriva **exclusivamente** del claim `sub` del JWT verificado.

### HU-02 — Registro mis activos

> Como usuario, quiero agregar un activo indicando nombre, plataforma, clase y valor en USD.

- **CA-02.1** `POST /api/v1/holdings` con body válido responde `201`, header `Location`, y el cuerpo
  del holding creado con `id` (UUID) y `createdAt`.
- **CA-02.2** Si `platform` no existe para el usuario, se crea automáticamente con `type = "Other"`
  **en la misma transacción**, y aparece en el siguiente `GET /api/v1/platforms`.
- **CA-02.3** Si `assetClass` no está entre las conocidas, se acepta igual (texto libre) y aparece en
  el siguiente `GET /api/v1/asset-classes`.
- **CA-02.4** `value` ≤ 0, ausente, `NaN`, `Infinity` o con más de 2 decimales → `400` con
  `problem+json` que enumera los campos inválidos.
- **CA-02.5** `name`, `platform` y `assetClass` vacíos o sólo whitespace → `400`. Se aplica `trim` y
  se colapsan espacios internos antes de validar longitud (máx. 120 caracteres cada uno).
- **CA-02.6** El valor se persiste como `NUMERIC(20,2)` — sin pérdida de precisión ni redondeo binario.

### HU-03 — Veo y limpio mi lista de activos

- **CA-03.1** `GET /api/v1/holdings` devuelve los holdings del usuario ordenados por `createdAt` asc.
- **CA-03.2** `GET /api/v1/holdings?assetClass=Crypto` filtra por clase exacta (case-sensitive).
- **CA-03.3** `GET /api/v1/holdings?platform=Binance` filtra por plataforma exacta.
- **CA-03.4** `DELETE /api/v1/holdings/{id}` responde `204`; un segundo `DELETE` del mismo id → `404`.
- **CA-03.5** `PATCH /api/v1/holdings/{id}` aplica sólo los campos presentes en el body; los ausentes
  no se modifican; un body `{}` es válido y no cambia nada (`200`).

### HU-04 — Gestiono mis plataformas

- **CA-04.1** `GET /api/v1/platforms` devuelve todas las plataformas del usuario, **incluidas las que
  hoy tienen saldo 0**, ordenadas por nombre.
- **CA-04.2** `POST /api/v1/platforms` con `{name, type}` responde `201`; nombre duplicado
  (comparación case-insensitive) → `409`.
- **CA-04.3** `DELETE /api/v1/platforms/{name}` con holdings asociados → `409` con un mensaje que
  indica cuántos holdings la referencian. Sin holdings → `204`.
- **CA-04.4** Un usuario nuevo (primer login) arranca con **cero** plataformas y cero holdings.
  Los datos semilla de `constants.ts` no se replican por usuario.

### HU-05 — Veo mi tablero consolidado

- **CA-05.1** `GET /api/v1/wealth/summary` devuelve `netWorth.usd` = suma exacta de los `value` del
  usuario, con 2 decimales.
- **CA-05.2** `byAssetClass` incluye `valueUsd`, `pct` (1 decimal) y `count`, ordenado por valor desc.
  La suma de `pct` está en `[99.5, 100.5]` cuando hay al menos un holding (tolerancia de redondeo).
- **CA-05.3** `byPlatform` incluye **todas** las plataformas del usuario, incluso con `valueUsd = 0`.
- **CA-05.4** Con cero holdings: `netWorth.usd = 0`, listas vacías, `liquidity.liquidPct = 0`,
  `ytd.growthPct = 0`. **Sin división por cero, sin `NaN`, sin `500`.**
- **CA-05.5** `liquidity.liquidPct` = (Σ valores de clases configuradas como líquidas / total) × 100,
  redondeado a 1 decimal; `illiquidPct = 100 − liquidPct`. Las clases desconocidas o creadas por el
  usuario cuentan como **ilíquidas** (replica la lógica actual de `WealthContext`).
- **CA-05.6** El conjunto de clases líquidas es configuración del servidor
  (`wealth.liquid-asset-classes`, default `Cash, Equity, Crypto, Index Fund`), no una constante en código.
- **CA-05.7** `ytd.growthPct` = ((neto actual − valor del primer snapshot del año en curso) / ese valor) × 100.
  Si no hay snapshot en el año en curso, se usa el snapshot más antiguo disponible; si no hay ninguno
  o el valor base es 0, `growthPct = 0` y `basis = "NO_BASELINE"`.
- **CA-05.8** `netWorth.ars = usd × fxRate`, con `fxRate.value` y `fxRate.asOf` en la respuesta.
  El origen del tipo de cambio es un puerto de salida sustituible, no una constante inyectada.

### HU-06 — Registro y reviso mi historia

- **CA-06.1** `POST /api/v1/wealth/snapshots` **sin body** calcula el neto actual en el servidor y
  crea el snapshot; responde `201` con `{id, capturedAt, totalValueUsd}`.
- **CA-06.2** El cliente **no** envía el importe: se calcula server-side (no es falsificable).
- **CA-06.3** `GET /api/v1/wealth/snapshots` devuelve la serie ordenada por `capturedAt` asc,
  cada punto con `changePctFromPrevious` (`null` en el primero).
- **CA-06.4** Dos snapshots en el mismo segundo → el segundo responde `409`. (Protege contra
  doble click; el frontend además deshabilita el botón mientras el request está en vuelo.)
- **CA-06.5** La respuesta trae `capturedAt` ISO-8601 UTC, **no** una etiqueta ya formateada:
  el formato de fecha es responsabilidad de la vista.

### HU-07 — Proyecto mi futuro

- **CA-07.1** `GET /api/v1/wealth/estimate?contribution=900&yieldPct=9&years=12` devuelve una serie de
  **`years + 1` puntos** (año 0 … año N). El punto del año 0 es igual al patrimonio actual.
- **CA-07.2** La fórmula es `FV = P·(1+r)^n + PMT·((1+r)^n − 1)/r`, con `r = yieldPct/100/12`,
  `n = años·12`; cuando `|r| < 1e-9` se usa el caso degenerado `P + PMT·n`.
- **CA-07.3** Los resultados coinciden con `frontend/src/lib/calculations.ts` dentro de una tolerancia
  de `0.01` para el conjunto de casos dorados definido en `tasks.md` T-31 (garantía de paridad
  durante la migración).
- **CA-07.4** Los hitos son parametrizables: `?milestones=150000,250000` (default `150000,250000`,
  máximo 5). Cada hito devuelve `status ∈ {ACHIEVED, REACHABLE, OUT_OF_HORIZON}`,
  `monthsRequired` (`null` si `OUT_OF_HORIZON`) y `targetMonth` (`YYYY-MM`, `null` si no aplica).
- **CA-07.5** El horizonte de búsqueda de hitos es `years·12` meses — un hito alcanzable en el año 30
  con `years=12` devuelve `OUT_OF_HORIZON`, no una fecha.
- **CA-07.6** Validación: `contribution ∈ [0, 1e9]`, `yieldPct ∈ [0, 100]`, `years ∈ [1, 50]`.
  Fuera de rango → `400`. **No hay estado que mutar**: el endpoint es idempotente y cacheable
  (`Cache-Control: private, max-age=30`).

### HU-08 — Errores predecibles

- **CA-08.1** Todos los errores usan `application/problem+json` (RFC 9457) con
  `type`, `title`, `status`, `detail`, `instance` y, en validaciones, un array `errors[{field, message}]`.
- **CA-08.2** Un `500` **nunca** expone `exception.message`, stack traces, SQL ni nombres de clases.
  Devuelve un `traceId` correlacionable con el log del servidor.
- **CA-08.3** Todo error se registra con `traceId`, `userId` y ruta; **nunca** con el JWT, el email
  ni los importes del usuario.

---

## 5. Requisitos no funcionales

| ID | Requisito | Verificación |
|---|---|---|
| NFR-1 | `GET /wealth/summary` responde en < 200 ms p95 con 1.000 holdings | Test de carga en CI (informativo) |
| NFR-2 | Las agregaciones (`summary`) se resuelven con `SUM/GROUP BY` en SQL, no cargando N filas en memoria | Test de repositorio + revisión |
| NFR-3 | Toda query de datos de usuario lleva `WHERE user_id = ?`; ningún repositorio expone un `findAll()` sin usuario | Test de arquitectura (Konsist) + revisión de firmas |
| NFR-4 | Cobertura de líneas ≥ 80 % en `domain/` y `application/`; ≥ 60 % global | Gate de JaCoCo en el build |
| NFR-5 | `domain/` no importa nada de `org.springframework`, `jakarta`, `com.fasterxml` ni de `infrastructure/` | Test de arquitectura (Konsist) |
| NFR-6 | El contrato OpenAPI publicado valida contra cada respuesta en los tests de integración | swagger-request-validator |
| NFR-7 | Sin secretos en el repo; toda config sensible por variable de entorno | gitleaks en CI |
| NFR-8 | Los importes monetarios son `BigDecimal`/`NUMERIC(20,2)` de punta a punta; `Double` está prohibido para dinero | Test de arquitectura + revisión |
| NFR-9 | Migraciones de esquema versionadas, idempotentes y ejecutadas al arranque (Flyway) | `flyway:validate` en CI |
| NFR-10 | CORS restringido a los orígenes configurados; sin `*` con `allowCredentials` | Test de integración |
| NFR-11 | El servicio arranca en local **sin base de datos** (perfil `dev` con Postgres en Testcontainers o Docker Compose) | `./gradlew bootRun` documentado |

---

## 6. Qué queda deliberadamente en el cliente

No todo lo que el frontend hace debe mudarse al backend. Estas decisiones son explícitas:

| Capacidad | Se queda en el cliente | Motivo |
|---|---|---|
| Formateo (`formatCurrency`, `formatPercentage`, nombres de mes) | Sí | Es presentación; el `locale` es del navegador. El backend devuelve números y fechas ISO. |
| Colores, tags, iniciales de plataforma (`ASSET_CLASS_COLORS`, …) | Sí | Tokens de diseño, no datos de negocio. |
| Generación de paths SVG (`generateLinePath`) | Sí | Depende del viewport, no del dominio. |
| Estado de UI: vista activa, filtro, plataforma seleccionada, modal abierto | Sí | Efímero por pestaña. |
| Sliders de `EstimateParams` | Sí (con debounce ~150 ms sobre el request) | Ver *Decisión abierta D1*. |
| Identidad del usuario para el `Sidebar`/`ProfileModal` (F22) | Sí | Ya está en la sesión de Supabase; un `GET /me` sería un round-trip sin información nueva. |

---

## 7. Decisiones abiertas

| ID | Pregunta | Recomendación | Impacto si cambia |
|---|---|---|---|
| **D1** | ¿La proyección (`/wealth/estimate`) debe ser server-side? Es matemática pura sin datos privados, disparada por sliders a alta frecuencia. | **Exponerla en el backend** (una sola fuente de verdad para la fórmula, es lo que pide este spec) **y** dejar el cálculo del cliente como fallback offline durante la migración, borrándolo una vez validada la paridad (CA-07.3). | Bajo: es un endpoint aislado. |
| **D2** | ¿Migrar el `localStorage` existente del usuario a su cuenta en el primer login? | **No en v1.** Los datos actuales son datos semilla de demo. Si se pide, se resuelve con un `POST /api/v1/holdings/bulk-import` de un solo uso, con confirmación explícita del usuario. | Medio: agrega un endpoint y una pantalla. |
| **D3** | ¿Se mantiene el módulo `Tasks`? El frontend **no tiene ninguna UI de tareas** — es superficie muerta, y hoy es además un bypass de autorización. | **Mantener el endpoint, re-scopearlo al usuario del JWT y marcarlo como candidato a borrado.** Si en 1 mes no hay UI, se borra (`tasks` + `TaskService` + `TaskRepository` + tabla). | Bajo. |
| **D4** | Origen del tipo de cambio USD/ARS (F12). Hoy es una constante `1050.0` inyectada por config — ya nació desactualizada, y ninguna vista lo muestra todavía. | Puerto `FxRateProvider` con adaptador `FixedFxRateProvider` (config) por defecto. Un adaptador HTTP contra una API real se agrega cuando exista una vista que lo muestre. | Bajo: el puerto ya lo aísla. |
| **D5** | ¿RLS en Postgres para las tablas nuevas, si el backend conecta con un rol privilegiado que la ignora? | **Sí, activarla igual** (defensa en profundidad: bloquea el acceso vía PostgREST/anon key), pero el control primario es el filtro por `user_id` en la aplicación. Ver `plan.md` §5.4. | Bajo. |

---

## 8. Riesgos

| ID | Riesgo | Prob. | Impacto | Mitigación |
|---|---|---|---|---|
| R1 | Divergencia de la matemática financiera entre cliente y servidor durante la migración | Alta | Medio | Casos dorados compartidos (CA-07.3, T-31); borrar `calculations.ts` financiero al cerrar la migración |
| R2 | Regresión de seguridad: un repositorio nuevo olvida el `WHERE user_id = ?` | Media | **Crítico** | Firmas de puerto que **exigen** `UserId` (imposible olvidarlo), test de arquitectura NFR-3, y un test de aislamiento por cada endpoint |
| R3 | Holdings cargados en moneda local pero guardados como USD, sin registro del tipo de cambio usado | Media | Medio | Fuera de alcance en v1; documentado. El campo se llama `valueUsd` explícitamente para que la ambigüedad no se propague |
| R4 | `Double` para dinero produce totales con centavos fantasma (`0.1+0.2`) | **Ya presente** | Medio | NFR-8: `BigDecimal` + `NUMERIC(20,2)` de punta a punta |
| R5 | El pooler de Supabase (IPv4/session) limita conexiones y tumba el arranque | Media | Alto | `maximum-pool-size` acotado, `HikariCP` con timeouts explícitos, health check de readiness que verifica la DB |
| R6 | Sobre-ingeniería: el spec introduce capas que nadie necesita | Media | Medio | `plan.md` §4.2 lista los patrones **rechazados** con su motivo; toda abstracción nueva necesita ≥2 implementaciones reales o un test que la exija |
| R7 | El JWT de Supabase cambia de algoritmo (HS256 legacy → claves asimétricas) y rompe la verificación | Baja | Alto | Verificación vía JWKS con rotación automática; test de integración con un JWKS mockeado |

---

## 9. Glosario

- **Holding**: una posición concreta del usuario (p.ej. "NVDA en Balanz, 10.557 USD").
- **Platform**: el lugar donde vive un holding (broker, banco, wallet, exchange). Propiedad del usuario.
- **Asset class**: categoría del activo. Texto libre desde que se abrió el alta en el frontend.
- **Snapshot**: foto del patrimonio neto total en un instante, guardada para construir la serie histórica.
- **Neto (net worth)**: suma de los `valueUsd` de todos los holdings del usuario.
- **Líquido**: holdings cuya clase pertenece al conjunto configurado como líquido.
- **YTD**: variación porcentual desde el primer snapshot del año en curso hasta el neto actual.
