# Tareas 001 — Backend completo para el frontend

Derivadas de [`spec.md`](spec.md), [`plan.md`](plan.md) y [`data-model.md`](data-model.md).

**Convenciones**

- `[P]` = puede ejecutarse en paralelo con las otras `[P]` del mismo bloque (tocan archivos distintos).
- Cada tarea lista sus **archivos**, su **DoD** (definition of done) y el criterio de aceptación de
  `spec.md` que cierra.
- Cada fase deja `main` desplegable. `./gradlew check` debe quedar verde al final de cada fase.
- El orden dentro de una fase es de dependencias, no de preferencia.

## Estado de ejecución (actualizado 2026-08-24)

**Fases 0 a 6: implementadas y verificadas** (JDK 21 + Gradle corridos localmente; sin Docker
disponible, así que lo que depende de Testcontainers/Docker Compose quedó verificado por
compilación, no por ejecución — ver detalle abajo).

**Desvío deliberado respecto al plan**: T-25 (borrar el KVS) no es separable de T-40/T-41/T-42/T-43
(rewire de Holdings) sin dejar el árbol sin compilar o cambiar el contrato HTTP en silencio —
borrar `KvHoldingRepository` obliga a que `HoldingController`/`HoldingUseCase`/`HoldingService`
compilen contra el nuevo dominio. Por eso, en Fase 2 se adelantó lo mínimo indispensable: los tipos
de dominio de Holdings y un `HoldingUseCase`/`HoldingController` que compilaban, pero todavía sin
comandos dedicados (`createHolding`/`updateHolding` tomaban los campos sueltos, no
`CreateHoldingCommand`/`PatchHoldingCommand`) ni filtros por querystring. Esa versión intermedia
quedó documentada acá como "T-40/42/43 resueltos" — **eso fue impreciso**: lo que se cerró en Fase 2
fue el mínimo para que compilara sin el KVS, no el shape final de esas tareas.

En Fase 3 se completaron correctamente:
- T-40 (puerto de entrada con comandos propios: `CreateHoldingCommand`/`PatchHoldingCommand` en vez
  de parámetros sueltos — igual sigue siendo **solo para Holdings**; el resto de
  `domain/port/inbound/*.kt` mantiene la fuga A1 deliberadamente hasta que se toquen en su propia
  fase).
- T-41 (alta implícita de plataforma en la misma llamada, atómica vía `@Transactional`).
- T-42 (DTOs propios en vez de devolver el dominio; `id` es UUID string, no `Long`).
- T-43 (filtros `?assetClass=`/`?platform=` en `GET /holdings`, con `@RequestParam` opcionales).
- T-44 (Platforms CRUD completo: `GET/POST/PATCH/DELETE /api/v1/platforms`, con `409 Conflict` en
  rename duplicado o borrado con holdings asociados).
- T-45 (`GET /api/v1/asset-classes`, devolviendo `defaults`/`inUse`/`all`).

**T-46 (tests de aislamiento A/B) queda solo parcialmente cerrado**: `PlatformControllerTest` y
`AssetClassControllerTest` son `@WebMvcTest` con `PlatformUseCase`/`AssetClassUseCase` mockeados —
verifican contrato HTTP, no aislamiento real contra Postgres. No se escribió ningún test nuevo de
Testcontainers para aislamiento cruzado de usuario en `JdbcPlatformRepository`,
`JdbcSnapshotRepository` ni `JdbcWealthAggregationAdapter`; siguen sin tests dedicados (ver abajo).

**Fase 4: implementada y verificada.** Cierra HU-05/HU-06 corrigiendo las fugas **A2**
(`WealthQueryService` dependía de `HoldingUseCase`/`PlatformUseCase`/`HistoryUseCase`, puertos de
**entrada**) y **A3** (historial hardcodeado en `HistoryService`):
- T-50: `WealthQueryService` reescrito contra puertos de **salida** (`WealthAggregationPort`,
  `SnapshotRepository`, `FxRatePort`) — cero dependencia de otros casos de uso. De paso se borró
  `HoldingUseCase.getTotalNetWorthUSD`, que existía solo para este llamador (el TODO que dejó
  Fase 2 apuntando a esto). **Desvío de nombres respecto al plan**: se mantuvo `WealthUseCase`/
  `WealthQueryService` (no `QueryWealthUseCase`) por consistencia con `HoldingUseCase`/
  `PlatformUseCase`/`AssetClassUseCase` ya establecidos; snapshots se resolvió con un
  `SnapshotUseCase`/`SnapshotService` nuevo, mismo criterio, no `TrackHistoryUseCase`.
- T-51 y T-52 ya estaban resueltos en la infraestructura de Fase 2 (`LiquidityPolicy`,
  `WealthProperties.liquidAssetClasses`, `SnapshotRepository.findFirstOfYear/findEarliest`) — lo
  que faltaba era el consumidor. Se agregó `domain/model/YtdGrowth.kt` (con un campo `basis:
  YtdBasis` que el sketch de `data-model.md` no explicitaba, necesario para el enum
  `YEAR_START_SNAPSHOT|EARLIEST_SNAPSHOT|NO_BASELINE` del contrato) y
  `domain/service/YtdGrowthCalculator.kt`.
- T-53: `GET /wealth/summary` reescrito al contrato completo de `openapi.yaml` (`netWorth.ars`/
  `fxRate`, `ytd`, `liquidity`, `byAssetClass`/`byPlatform` con `pct`) — es un cambio de contrato
  HTTP incompatible con la forma anterior (`totalNetWorthUSD` plano), igual que Holdings/Platforms
  en Fases 2-3; el cutover del frontend es Fase 6.
- T-54: cubierto con un test dedicado a portafolio vacío en `WealthQueryServiceTest` y en
  `WealthControllerTest` — sin `NaN`, sin `500`, `liquidity.liquidPct = 0`, `ytd.basis =
  "NO_BASELINE"`.
- T-55: `SnapshotUseCase`/`SnapshotService` — `POST /wealth/snapshots` calcula el neto server-side
  (nunca del cliente) y trunca `capturedAt` al segundo; el doble-click en el mismo segundo
  responde `409` vía un `existsAt()` de precheck contra la constraint `snapshots_user_instant_uk`
  (ver `data-model.md` §2) — es *check-then-act*, no atómico; la constraint de la base es la
  garantía real contra una carrera concurrente genuina (documentado con `ponytail:` en
  `SnapshotService.kt`). `GET /wealth/snapshots` calcula `changePctFromPrevious` par a par
  (`Money.growthPctFrom`, nuevo, reutilizado también por `YtdGrowthCalculator`).
- T-56: se borraron `HistoryService`, `HistoryUseCase` y `HistorySnapshot` (el historial hardcodeado
  de A3). La mitad de T-56 sobre `PlatformService` ya no aplicaba — sus listas hardcodeadas se
  habían reemplazado por `PlatformRepository` real en Fases 2/3.

**Fase 5: implementada y verificada.** Cierra HU-07 migrando `/wealth/estimate` a `GET`, dominio
propio y baja de `WealthCalculationService`:
- T-60: `domain/model/projection/ProjectionParams.kt` (factory `of()` con los invariantes de
  CA-07.6) y `Milestone.kt` (con `MilestoneStatus` y `ProjectionPoint`, colocados en el mismo
  archivo por simplicidad — son dos data class chicas sin identidad propia, no ameritan separarse).
- T-61: `ProjectionUseCase`/`ProjectionService` nuevos (mismo criterio de nombres que Fase 4, no
  `ProjectWealthUseCase`) — el principal es el neto actual del usuario vía `WealthAggregationPort`,
  salvo que la query pase `principal` (simulaciones what-if). Serie de `years + 1` puntos (año 0 =
  principal) vía `domain/service/ProjectionCalculator.kt`, que reutiliza
  `CompoundInterestCalculator` (ya migrado en Fase 2).
  `WealthQueryService` ya no depende de `WealthCalculationService` — se eliminó esa dependencia
  junto con `WealthUseCase.calculateEstimate`.
- T-62: hitos con horizonte `years·12` vía `ProjectionCalculator.milestones()` — `ACHIEVED` con mes
  0 sale gratis de `CompoundInterestCalculator.monthsToReach` (que ya evalúa el mes 0), no hace
  falta un caso especial.
- T-63: `GET /wealth/estimate` reemplaza el `POST`, con `Cache-Control: private, max-age=30`.
  Validación de rango (`contribution`/`yieldPct`/`years`/`milestones`) vía `@Validated` +
  `jakarta.validation` en los `@RequestParam` — **hallazgo no trivial**: con `@Validated` en un
  controller, Spring Boot 3.3.2 valida vía el interceptor AOP clásico
  (`MethodValidationInterceptor`) y lanza `jakarta.validation.ConstraintViolationException`, **no**
  `HandlerMethodValidationException` (la implementación nueva de Spring MVC 6.1) — se confirmó
  empíricamente con un test real, no por lectura de documentación; el handler en
  `ApiExceptionHandler` está escrito para el tipo que realmente se lanza. De paso se agregó un
  handler para `IllegalArgumentException` → `400` (antes cualquier `require()` de dominio —
  `ProjectionParams`, `Holding`, `Platform`, etc. — cascadeaba al catch-all y respondía `500`; era
  un gap latente desde Fase 0, nunca disparado porque `jakarta.validation` en los DTOs de
  `@RequestBody` ya atajaba la mayoría de los casos antes de llegar al dominio).
- T-64: fixture dorada ampliada en `CompoundInterestCalculatorTest` (no como JSON separado — no hay
  runner que comparta casos entre Kotlin y TypeScript en este monorepo) con valores recalculados
  exactamente contra `frontend/src/lib/calculations.ts` vía Node.js, tolerancia `0.01` real, no un
  rango amplio como quedó en Fase 2.
- T-65: `application/service/WealthCalculationService.kt` y su test, borrados — la lógica vive en
  `domain/service/CompoundInterestCalculator.kt` (Fase 2) y `ProjectionCalculator.kt` (Fase 5).

**Fase 6: implementada y verificada.** Corte completo del frontend contra el backend real — cero
`localStorage`, cero agregación client-side.
- **Corrección de contrato encontrada antes de empezar**: `HoldingResponse`/`CreateHoldingRequest`/
  `UpdateHoldingRequest` seguían usando `cls`/`value` (Fase 2) en vez de `assetClass`/`valueUsd` que
  especifica `openapi.yaml`, y a `HoldingResponse` le faltaban `createdAt`/`updatedAt`. El comentario
  en el código decía que el rename quedaba para "Fase 3 (T-42)" pero nunca se hizo. Se corrigió en
  el backend (DTOs + `HoldingController` + su test) antes de tocar el frontend — construir el
  frontend contra el contrato viejo hubiera significado deshacerlo después.
- T-70: `lib/api.ts` nuevo — `fetch` tipado sin librerías nuevas, token de `supabase.auth.getSession()`
  en cada request (no cacheado en contexto, así siempre es el token vigente), `ApiError` con
  `status`/`errors[]` parseados de `application/problem+json`. Un `401` fuerza `supabase.auth.signOut()`,
  lo que dispara re-login vía el propio `onAuthStateChange` que `AuthContext` ya escuchaba.
- T-71: `types/wealth.ts` reescrito 1:1 contra `openapi.yaml` (`Holding`, `Platform`,
  `WealthSummary`, `Snapshot`, `Projection`, etc.). `tsc --noEmit` y `next build` limpios.
- T-72: `WealthContext` reescrito — `netWorthFormatted`/`liquidityPct`/`illiquidPct`/
  `classDistribution`/`platformDistribution` ahora se derivan de `GET /wealth/summary` (agregación
  SQL), no de recorrer `holdings` en el cliente; esto es exactamente lo que Fase 4 (A5) preparó.
  Las propiedades expuestas a las vistas no cambiaron de nombre salvo `history` → `snapshots`
  (forzado por el cambio real de forma: HU-06 reemplaza el historial falso mensual por snapshots
  reales). Toda mutación (`addHolding`/`deleteHolding`/`takeSnapshot`) hace `POST`/`DELETE` y
  refresca todo el estado desde el servidor — sin actualización optimista: es un dashboard personal
  de bajo tráfico, no vale la complejidad de reconciliar estado optimista con lo que el servidor
  realmente persistió.
- T-73: skeleton inicial + pantalla de error con reintento en `page.tsx` (`loading`/`loadError` del
  contexto); error inline en `AddAssetModal` y `AssetsView` (delete); botón de snapshot deshabilitado
  mientras está en vuelo en `HistoryView`.
- T-74: `EstimateView` contra `GET /wealth/estimate`, debounce de 150ms vía un hook chico local (sin
  librería nueva), milestones genéricos (ya no hardcodea "$150k"/"$250k" en el JSX, los deriva de
  `milestones[].amountUsd`).
- T-75: `HistoryView` usa `changePctFromPrevious` del servidor y formatea la etiqueta desde
  `capturedAt` con `toLocaleDateString` (sin `MONTH_NAMES` ni fórmula propia).
- T-76: borrados `fvYears`/`fvMonths`/`monthsToReach`/`dateLabelFromMonths` de `calculations.ts` y
  `INITIAL_HOLDINGS`/`INITIAL_PLATFORMS`/`INITIAL_HISTORY`/`ASSET_CLASS_OPTIONS`/`MONTH_NAMES` de
  `constants.ts` (los dos últimos no estaban en la lista original de la tarea, pero quedaron muertos
  por el mismo motivo: `ASSET_CLASS_OPTIONS` lo reemplaza `GET /asset-classes`, `MONTH_NAMES` solo
  lo usaba `dateLabelFromMonths`). `generateLinePath`/`formatCurrency`/`formatPercentage` quedan,
  son renderizado puro, no lógica financiera.
- T-77: `.env.example` + sección de setup del README actualizada (Supabase + `NEXT_PUBLIC_API_BASE_URL`,
  con nota de que en Vercel debe apuntar al Render deployado, no a `localhost`).
- T-78: "Skip login (dev)" ahora gateado por `process.env.NODE_ENV !== 'production'` en `page.tsx`
  (antes se ofrecía siempre, incluso en build de producción).

**Verificación real hecha, no solo compilación**: sin Docker ni un deploy de Render funcional
disponibles en este entorno (ver nota de Render más abajo), levanté un mock HTTP server chico
(Node, sin dependencias) que replica las formas exactas de respuesta del backend real, y probé en
el browser: carga inicial (summary/holdings/platforms/asset-classes/snapshots en paralelo),
Dashboard con métricas y distribuciones correctas, Assets con filtros, Platforms con drilldown,
Estimate con fetch debounced y milestones con fecha correcta, History con snapshot nuevo, alta y
borrado de un holding de punta a punta, y la pantalla de error con reintento contra un backend
inalcanzable (probado primero contra el puerto real 8080 sin nada corriendo, antes de armar el
mock). `tsc --noEmit` y `next build` limpios. `next lint` no está configurado en este proyecto
(pregunta interactiva de setup nunca respondida en un `git init`/`create-next-app` previo) — no es
una regresión de esta fase, ya estaba así.

**No verificado por falta de Docker en este entorno** (escrito y compilado, pero no ejecutado):
- `dev` profile vía `spring-boot-docker-compose` (`backend/compose.yaml`).
- Todo test que extiende `PostgresTestBase` (Testcontainers): `BaseWealthApplicationTests`,
  `JdbcHoldingRepositoryTest`. `JdbcPlatformRepository` (incluyendo el nuevo método `update()` de
  Fase 3), `JdbcSnapshotRepository`, `JdbcWealthAggregationAdapter` y `JdbcTaskRepository` no tienen
  tests dedicados todavía.
- `jacocoTestCoverageVerification`: el task depende de `test`, y `test` falla en este entorno por
  los 4 tests Docker-only de arriba (no hay forma de separar "corré jacoco" de "que pasen todos los
  tests" sin tocar `build.gradle.kts`) — cobertura no confirmada por el gate, solo inspeccionada
  manualmente en el código nuevo de Fases 4 y 5 (services con mocks vía MockK, dominio con tests
  puros).
- **El deploy de Render sigue en el commit `077691d`** (previo a todo este trabajo de Fases 0-6),
  `status: build_failed` (el problema de path del Dockerfile ya documentado en memoria). El backend
  real de producción no sirve nada de lo construido en esta sesión todavía — hace falta pushear y
  resolver ese deploy antes de que el frontend cortado en Fase 6 funcione contra el Render real.

**Fase 7: implementada.** Cierre de seguridad y de gates de CI que quedaban pendientes:
- T-80: `TaskController`/`TaskUseCase`/`TaskRepository`/`JdbcTaskRepository` re-scopeados al `@CurrentUser`
  del JWT — corrige el bypass de autorización de A10 (`userId` ya no se acepta en el body ni en
  el query param). Se mantiene el endpoint (no se borra, D3), pero queda `@Hidden` de
  springdoc/`contracts/openapi.yaml`: sigue siendo candidato a borrado, sin UI, y no forma parte
  del contrato publicado.
- T-81: la RLS de `platforms`/`holdings`/`net_worth_snapshots` **ya estaba** en `V2__wealth_tables.sql`
  desde Fase 2 (no en un `V5` separado como sugería el plan original — desvío de nombres, no de
  contenido). La de `tasks`/`profiles` vive en `supabase/schema.sql`, aplicada fuera de Flyway.
  **Hallazgo fuera del alcance de T-80…T-86**: el proyecto Supabase real (`vffsdgqqyqcbmkehnpxx`,
  "BASE Project") hoy sólo tiene la tabla `kv_store` — ni `supabase/schema.sql` (`tasks`/`profiles`)
  ni ninguna migración de `V2` en adelante se aplicaron nunca contra él. Como
  `V4__tasks_indexes.sql` da por sentado que `tasks` ya existe (vía `schema.sql`), el primer
  `flyway migrate` real contra este proyecto **va a fallar** en `V4` a menos que `schema.sql` se
  corra a mano primero. No resuelto: requiere decidir si `schema.sql` se aplica manualmente antes
  del primer deploy o si esas tablas se pliegan a una migración de Flyway propia.
- T-82: `swagger-request-validator-mockmvc:2.46.1` (`support/ContractValidation.kt`) valida las
  respuestas reales de `HoldingControllerTest`/`PlatformControllerTest`/`AssetClassControllerTest`/
  `WealthControllerTest` contra `contracts/openapi.yaml` (no se agregó a `TaskControllerTest`: Tasks
  no está en el contrato). Dos bugs de la librería encontrados y confirmados empíricamente antes de
  trabajarlos alrededor (no por lectura de changelog): `oneOf` con una rama `{type: 'null'}` bajo
  OpenAPI 3.1 hace *match* con cualquier valor, tipo incorrecto incluido — se cambiaron las 6
  ocurrencias de `oneOf`-nullable del contrato a `anyOf` (semánticamente equivalente para este caso,
  sin el bug); y `allOf` + `additionalProperties` reporta violaciones cruzadas entre ramas (bug
  reconocido por la propia librería en su README) — mitigado con un `LevelResolver` que ignora sólo
  esa regla, verificado de que no oculta violaciones reales (`errors[].message` faltante se sigue
  detectando). De paso se encontró y corrigió un bug real de contrato: `netWorth.ars` estaba en
  `required` pero es nullable y Jackson (`default-property-inclusion: non_null`) omite el campo
  entero cuando es `null`, no manda `"ars": null` — se sacó de `required`.
- T-83: `contractDriftCheck` (task de Gradle) + `contract/OpenApiContractDriftTest.kt` comparan
  `contracts/openapi.yaml` contra `docs/api/openapi.json` a nivel de `{método, path}` (no un diff
  profundo de schema — dos generadores distintos producen formas distintas-pero-equivalentes; el
  contract test de T-82 ya cubre la forma de los campos donde importa). Verificado el mecanismo
  comparando el contrato contra sí mismo (pasa) y contra el `openapi.json` desactualizado que había
  en el repo (falla, como se espera) — no se pudo correr `generateOpenApiDocs` de punta a punta
  porque bootea la app real y `spring-boot-docker-compose` intenta levantar Docker (no disponible en
  este entorno); va a correr completo en CI.
- T-84: `.github/workflows/ci.yml` (spotlessCheck → detekt → test → jacocoTestCoverageVerification →
  contractDriftCheck, más un job de `gitleaks`) y `.github/workflows/dependency-check.yml`
  (OWASP dependency-check semanal, `cron` lunes 06:00 UTC). De paso, dos arreglos que este mismo
  pipeline necesitaba para no romper en un runner Linux: `backend/gradlew` estaba trackeado en git
  como `100644` (no ejecutable — nunca se había corrido `./gradlew` fuera de Windows) y
  `forkedSpringBootRun` (plugin de OpenAPI) disparaba un error de validación de tareas de Gradle 8.7
  por una dependencia implícita no declarada contra `compileTestKotlin`/`compileTestJava`.
- T-85: `backend/README.md` — la sección "💾 Persistencia (KVS)" (describía `FileKvStore`/
  `PostgresKvStore`, borrados en Fase 2) se reemplazó por la persistencia real (Flyway + perfiles
  `dev`/`test`/`prod`), más una sección nueva de autenticación. `README.md` raíz — JDK corregido a
  21 exacto, nota de que `dev` necesita Docker, y mención de `contracts/openapi.yaml` como fuente de
  verdad del diseño. `frontend/README.md` ya estaba al día desde Fase 6 (T-77), sin cambios.
- T-86: `backend/scripts/seed-dev.sql`, corrido a mano contra un `user_id` real de `auth.users`
  (parámetro psql `-v dev_user_id=...`), reutiliza los datos de ejemplo que tenía
  `frontend/src/lib/constants.ts` antes de Fase 6. Deliberadamente fuera de
  `src/main/resources/db/migration` para que Flyway nunca lo corra (CA-04.4).

**Verificado en este entorno**: `./gradlew compileKotlin compileTestKotlin` limpio,
`./gradlew spotlessCheck` y `./gradlew detekt` en verde, `./gradlew test` → 89 tests, sólo los 4
ya conocidos por falta de Docker fallan (`BaseWealthApplicationTests`, 3 de
`JdbcHoldingRepositoryTest`) y los 3 `@Disabled` de Konsist de siempre aparecen como skipped —
ningún test nuevo ni existente se rompió. `jacocoTestCoverageVerification` y la ejecución completa
de `contractDriftCheck`/CI en GitHub Actions (que sí tiene Docker) quedan pendientes de un run real.

---

## Fase 0 — Cimientos (sin cambios funcionales)

| # | Tarea | Archivos | DoD |
|---|---|---|---|
| T-01 | Subir la toolchain a JDK 21 y activar `allWarningsAsErrors` | `backend/build.gradle.kts` | Compila sin warnings |
| T-02 `[P]` | Spotless + ktlint | `build.gradle.kts`, `.editorconfig` | `./gradlew spotlessCheck` falla ante desvíos de estilo |
| T-03 `[P]` | detekt con config propia | `build.gradle.kts`, `backend/config/detekt.yml` | El build falla con issues de severidad `error` |
| T-04 `[P]` | JaCoCo con umbrales por paquete | `build.gradle.kts` | `jacocoTestCoverageVerification` exige 80 % en `domain`/`application`, 60 % global |
| T-05 | Konsist + los 6 tests de arquitectura de `plan.md` §8.2 | `src/test/.../architecture/ArchitectureTest.kt` | Un `import org.springframework` en `domain/` rompe el build (NFR-5) |
| T-06 | Flyway + Docker Compose de Postgres para dev | `build.gradle.kts`, `compose.yaml`, `src/main/resources/db/migration/` | `./gradlew bootRun` levanta con la DB de compose y migra |
| T-07 | Testcontainers con `@ServiceConnection` para el perfil `test` | `src/test/.../support/PostgresTestBase.kt` | Un test de integración de humo pasa contra Postgres real |
| T-08 | Perfiles `dev`/`test`/`prod` y `WealthProperties` con `@ConfigurationProperties` | `application.yml`, `infrastructure/config/WealthProperties.kt` | Sin `@Value` sueltos; la config está tipada y validada al arrancar |

---

## Fase 1 — Seguridad (HU-01)

| # | Tarea | Archivos | DoD / CA |
|---|---|---|---|
| T-10 | Dependencia `spring-boot-starter-oauth2-resource-server` | `build.gradle.kts` | — |
| T-11 | `SecurityConfig`: stateless, CSRF off, matchers públicos, `oauth2ResourceServer` | `.../security/SecurityConfig.kt` | Todo `/api/v1/**` sin token → `401` · **CA-01.1** |
| T-12 | `JwtDecoder` contra el JWKS de Supabase + validadores de `iss` y `aud` | `.../security/SupabaseJwtDecoderFactory.kt` | Token con `iss`/`aud` incorrecto, expirado o mal firmado → `401` · **CA-01.2** |
| T-13 | `UserId` + `@CurrentUser` + `HandlerMethodArgumentResolver` | `domain/model/UserId.kt`, `.../security/CurrentUserArgumentResolver.kt` | `@CurrentUser userId: UserId` resuelve al `sub` del token · **CA-01.5** |
| T-14 | `ApiExceptionHandler` con `ProblemDetail` (RFC 9457) + `traceId`; `401`/`403` también en `problem+json` | `.../web/error/` | Un `500` no expone `ex.message` · **CA-08.1, CA-08.2, A6** |
| T-15 | Filtro de `X-Request-Id` → MDC (`traceId`, `userId`) + logging estructurado | `.../config/RequestIdFilter.kt`, `logback-spring.xml` | Los logs traen `traceId` y `userId`, nunca token ni email · **CA-08.3** |
| T-16 | CORS por config, sin comodines con credenciales | `.../config/WebConfig.kt` | Test de integración de preflight · **NFR-10** |
| T-17 | Test slice de seguridad reutilizable (`jwt().jwt { it.subject(...) }`) | `src/test/.../support/WithUser.kt` | Los `@WebMvcTest` autentican en una línea |

---

## Fase 2 — Dominio y persistencia (base de HU-02…HU-07)

| # | Tarea | Archivos | DoD / CA |
|---|---|---|---|
| T-20 | `Money` con `BigDecimal(2, HALF_UP)`, `plus`, `times`, `percentOf` (null-safe ante total 0) | `domain/model/Money.kt` | Tests de redondeo: `33.33×3`, `0.1+0.2`, `percentOf(ZERO)` · **NFR-8, CA-05.4** |
| T-21 `[P]` | Value objects `HoldingId`, `AssetClass`, `PlatformName`, `PlatformType`, `FxRate` (con `UNAVAILABLE`) | `domain/model/` | Normalización (trim/colapso) testeada; se borra `Enums.kt` (los `typealias`) |
| T-22 | Entidades `Holding`, `Platform`, `NetWorthSnapshot` con factories e invariantes | `domain/model/` | No se puede construir una entidad inválida · **CA-02.5** |
| T-23 | Puertos de salida: `HoldingRepository`, `PlatformRepository`, `SnapshotRepository`, `WealthAggregationPort`, `FxRatePort`, `ClockPort` — **todos exigen `UserId`** | `domain/port/outbound/` | Konsist verifica que ninguna firma lo omita · **NFR-3** |
| T-24 | Migración `V2__wealth_tables.sql` | `src/main/resources/db/migration/` | Tablas, checks, FK compuesta e índices de `data-model.md` §2 |
| T-25 | Migración `V3__drop_kv_store.sql` + **borrar** `KvStore`, `FileKvStore`, `PostgresKvStore`, `KvHoldingRepository`, `KvTaskRepository`, `backend/data/` | varios (borrados) | `grep -r KvStore backend/src` sale vacío · **plan.md §2** |
| T-26 | `JdbcHoldingRepository` con `JdbcClient` + row mappers | `.../persistence/` | Tests con Testcontainers, incluido el de aislamiento A/B · **CA-01.3, CA-01.4** |
| T-27 `[P]` | `JdbcPlatformRepository` con resolución case-insensitive del nombre + `ensureExists` | `.../persistence/` | El alta implícita es idempotente bajo concurrencia · **CA-02.2, CA-04.2** |
| T-28 `[P]` | `JdbcSnapshotRepository` | `.../persistence/` | Duplicado en el mismo instante → violación de la unique → `409` · **CA-06.4** |
| T-29 | `JdbcWealthAggregationAdapter` con las consultas de `data-model.md` §3 | `.../persistence/` | Cero `findAll()` en el camino del summary · **NFR-2, A5** |
| T-30 | `FixedFxRateAdapter` detrás de `FxRatePort` | `.../fx/` | El rate sale de config con su `asOf`; hay un fake para tests · **CA-05.8, D4** |
| T-31 | **Casos dorados de paridad financiera**: fixture compartida con los valores que hoy produce `frontend/src/lib/calculations.ts` | `src/test/resources/golden/projection-cases.json` | ≥ 12 casos: `yieldPct=0`, `years=1`, `years=50`, hito ya alcanzado, hito inalcanzable, principal 0 · **CA-07.3, R1** |
| T-32 | `CompoundInterestCalculator` y `LiquidityPolicy` en `domain/service`, migrando la lógica de `WealthCalculationService` | `domain/service/` | Los casos dorados pasan dentro de 0.01 · **CA-07.2, CA-05.5** |

---

## Fase 3 — Holdings, plataformas y clases (HU-02, HU-03, HU-04)

| # | Tarea | Archivos | DoD / CA |
|---|---|---|---|
| T-40 | Puerto `ManageHoldingsUseCase` con comandos de dominio (**sin DTOs**) | `domain/port/inbound/` | Corrige la fuga **A1** |
| T-41 | `HoldingApplicationService` `@Transactional`; alta implícita de plataforma en la misma transacción | `application/` | **CA-02.2** |
| T-42 | `HoldingController` + DTOs + mappers; `PUT` → `PATCH` | `.../web/` | Corrige **A7** · **CA-03.5** |
| T-43 | Filtros `?assetClass=` y `?platform=` vía `HoldingFilter` | `.../web/`, `.../persistence/` | **CA-03.2, CA-03.3** (F5, F6) |
| T-44 `[P]` | `ManagePlatformsUseCase` + servicio + controlador (`GET`, `POST`, `PATCH`, `DELETE`) | varios | **CA-04.1…CA-04.3** (F7, F8) |
| T-45 `[P]` | `GET /api/v1/asset-classes` (defaults ∪ en uso) | `.../web/AssetClassController.kt` | **F9, F10** · sin tabla nueva |
| T-46 | Tests de integración de HU-02/03/04, incluido aislamiento A/B en **cada** endpoint | `src/test/` | **R2** |

---

## Fase 4 — Resumen e historia (HU-05, HU-06)

| # | Tarea | Archivos | DoD / CA |
|---|---|---|---|
| T-50 | `QueryWealthUseCase` + `WealthSummaryService` sobre puertos de **salida** (no de entrada) | `domain/port/inbound/`, `application/` | Corrige la fuga **A2** |
| T-51 | Liquidez configurable | `WealthProperties`, `domain/service/LiquidityPolicy.kt` | Clases desconocidas → ilíquidas · **CA-05.5, CA-05.6** |
| T-52 | YTD con los tres orígenes de baseline (`YtdGrowth` sellado) | `application/`, `.../persistence/` | **CA-05.7** (F16) |
| T-53 | `GET /wealth/summary` con el contrato de `openapi.yaml` | `.../web/WealthController.kt` | `byPlatform` incluye saldo 0 · **CA-05.3** (F14) |
| T-54 | Test de portafolio vacío para **todos** los campos del summary | `src/test/` | Sin `NaN`, sin división por cero, sin `500` · **CA-05.4** |
| T-55 `[P]` | `TrackHistoryUseCase`: `POST /wealth/snapshots` (importe server-side) y `GET` con `changePctFromPrevious` | varios | **CA-06.1…CA-06.5** (F17, F18, F19) |
| T-56 | Borrar `HistoryService` y `PlatformService` con sus listas hardcodeadas | (borrados) | Corrige **A3** |

---

## Fase 5 — Proyección (HU-07)

| # | Tarea | Archivos | DoD / CA |
|---|---|---|---|
| T-60 | `ProjectionParams` / `ProjectionSeries` / `Milestone` en el dominio | `domain/model/projection/` | Invariantes de rango en el factory · **CA-07.6** |
| T-61 | `ProjectWealthUseCase` + `ProjectionService`: serie de `years + 1` puntos desde el año 0 | varios | **CA-07.1** (F20) |
| T-62 | Hitos parametrizables con `MilestoneStatus` y horizonte `years·12` | `domain/service/` | **CA-07.4, CA-07.5** (F21) |
| T-63 | `GET /wealth/estimate` (reemplaza el `POST`), con `Cache-Control: private, max-age=30` | `.../web/WealthController.kt` | Idempotente y cacheable · **CA-07.6** |
| T-64 | Test de paridad contra la fixture dorada T-31 | `src/test/` | Diferencia ≤ 0.01 en los 12 casos · **CA-07.3** |
| T-65 | Borrar `application/service/WealthCalculationService.kt` (su lógica vive en `domain/service`) | (borrado) | Sin duplicación |

---

## Fase 6 — Corte del frontend

| # | Tarea | Archivos | DoD |
|---|---|---|---|
| T-70 | `lib/api.ts`: `fetch` tipado, `Authorization: Bearer`, parseo de `problem+json` → `ApiError` | `frontend/src/lib/api.ts` (nuevo) | Un `401` dispara re-login; sin librerías nuevas |
| T-71 | `types/wealth.ts` alineado al contrato (`cls`→`assetClass`, `value`→`valueUsd`, snapshots con `capturedAt`) | `frontend/src/types/wealth.ts` | Compila con `tsc --noEmit` |
| T-72 | `WealthContext`: cargar desde la API, borrar los helpers de `localStorage` y el `useEffect` de hidratación | `frontend/src/context/WealthContext.tsx` | Las propiedades expuestas al resto de la UI no cambian de nombre ni de forma |
| T-73 | Estados de carga y error: skeleton inicial, error inline en mutaciones, botón de snapshot deshabilitado en vuelo | vistas | Ninguna vista se rompe con la API caída |
| T-74 `[P]` | `EstimateView` contra `GET /wealth/estimate` con debounce de 150 ms | `frontend/src/components/views/EstimateView.tsx` | El gráfico se mantiene fluido moviendo los sliders |
| T-75 `[P]` | `HistoryView` usa `changePctFromPrevious` y formatea la etiqueta desde `capturedAt` | `frontend/src/components/views/HistoryView.tsx` | La tabla muestra lo mismo que hoy |
| T-76 | Borrar `fvYears`, `fvMonths`, `monthsToReach`, `dateLabelFromMonths` de `calculations.ts` y `INITIAL_*` de `constants.ts` | `frontend/src/lib/` | `grep -r "INITIAL_HOLDINGS\|localStorage" frontend/src` sale vacío |
| T-77 | `NEXT_PUBLIC_API_BASE_URL` + `.env.example` + README del frontend | varios | Un clon nuevo arranca siguiendo el README |
| T-78 | "Skip login (dev)" detrás de `NODE_ENV !== 'production'` (o eliminado) | `frontend/src/app/page.tsx` | Sin sesión no se sirve una UI que sólo puede dar `401` |

---

## Fase 7 — Limpieza y cierre

| # | Tarea | Archivos | DoD |
|---|---|---|---|
| T-80 | Resolver **D3** (Tasks): re-scopear al JWT (borrar `userId` del body, `WHERE user_id`) o borrar el módulo entero | `.../tasks/`, `supabase/` | Sin `userId` en ningún request body · **CA-01.5, A10** |
| T-81 | RLS de las tablas nuevas (`V5__wealth_rls.sql`) | `supabase/migrations/` | Verificado con el anon key: acceso denegado · **D5** |
| T-82 | Contract test: swagger-request-validator contra `contracts/openapi.yaml` | `src/test/` | Una respuesta fuera de contrato rompe el build · **NFR-6** |
| T-83 | CI: comparar `contracts/openapi.yaml` con `docs/api/openapi.json` generado | `.github/workflows/ci.yml` | Divergencia = build rojo · **plan.md §8.4** |
| T-84 | OWASP dependency-check + gitleaks en CI | `.github/workflows/` | Job semanal + gate en push · **NFR-7** |
| T-85 | Actualizar `README.md` raíz, `backend/README.md` (sección KVS obsoleta) y `frontend/README.md` | varios | Ningún README describe algo que ya no existe |
| T-86 | `backend/scripts/seed-dev.sql` fuera de las migraciones; sin semillas en producción | `backend/scripts/` | Un usuario nuevo arranca vacío · **CA-04.4** |

---

## Orden crítico

```
Fase 0 ──▶ Fase 1 ──▶ Fase 2 ──┬──▶ Fase 3 ──┬──▶ Fase 6 ──▶ Fase 7
                                ├──▶ Fase 4 ──┤
                                └──▶ Fase 5 ──┘
```

Las fases 3, 4 y 5 son independientes entre sí una vez terminada la 2 (dominio + persistencia +
seguridad). La fase 6 necesita las tres cerradas: hasta que el backend no sirva todo, el frontend no
puede soltar el `localStorage` sin perder funcionalidad.

## Estimación

| Fase | Tareas | Esfuerzo relativo |
|---|---|---|
| 0 — Cimientos | 8 | M |
| 1 — Seguridad | 8 | M |
| 2 — Dominio y persistencia | 13 | **L** |
| 3 — Holdings/plataformas | 7 | M |
| 4 — Summary/historia | 7 | M |
| 5 — Proyección | 6 | S |
| 6 — Frontend | 9 | M |
| 7 — Limpieza | 7 | S |

La fase 2 es la más grande y la que más desbloquea: `Money`, los value objects y los puertos con
`UserId` obligatorio son los que hacen que los bugs de dinero y de aislamiento sean **imposibles de
escribir**, no sólo improbables.
