# Plan técnico 001 — Arquitectura, patrones y calidad

Complemento de [`spec.md`](spec.md) (el *qué*). Este documento es el *cómo*.

**Stack fijado:** Kotlin 1.9 · JDK 21 (toolchain) · Spring Boot 3.3 · Gradle Kotlin DSL ·
PostgreSQL (Supabase) · Flyway · springdoc-openapi 2.6 · JUnit 5 + MockK + Testcontainers.

---

## 0. Principios (la "constitution" de este backend)

Estos principios son el criterio de aceptación de cualquier PR. Están antes que el diseño porque
mandan sobre él.

1. **El dominio no sabe que existe el mundo exterior.** `domain/` es Kotlin puro: sin Spring, sin
   Jackson, sin JDBC, sin `jakarta.*`. Si hay que anotarlo para que funcione, va en otra capa.
2. **Las dependencias apuntan hacia adentro.** `infrastructure → application → domain`. Nunca al revés.
3. **Un tipo por concepto, no un `String` por concepto.** `UserId`, `Money`, `PlatformName` son tipos.
   Un `fun get(a: String, b: String)` donde los dos argumentos se pueden intercambiar sin que el
   compilador se queje es un bug esperando.
4. **La identidad del usuario es un parámetro explícito**, no un `ThreadLocal` ni un `SecurityContext`
   leído desde el fondo de la pila. Un caso de uso que necesita saber quién llama lo declara en su firma.
5. **No se agrega una abstracción hasta que exista la segunda implementación** —o un test que la
   exija. Una interfaz con un solo implementador y sin test que la sustituya es ruido.
6. **El código más barato es el que no se escribe.** Antes de agregar una dependencia: ¿lo hace la
   stdlib de Kotlin? ¿lo hace Spring Boot ya? ¿lo hace Postgres? Recién ahí, la dependencia.
7. **Dinero es `BigDecimal` con escala explícita.** Nunca `Double`, nunca `Float`.
8. **Todo lo que se afloja a propósito se marca.** Un atajo consciente lleva un comentario que nombra
   su techo y el camino de salida, no un silencio que dentro de seis meses parece ignorancia.
9. **Un test por cada regla que puede romperse en silencio.** Redondeos, límites, aislamiento entre
   usuarios, caso vacío.

---

## 1. Diagnóstico de la arquitectura actual

Lo bueno: la separación en `domain / application / infrastructure` ya existe, los puertos de entrada
y salida están declarados, y `PostgresKvStore` / `FileKvStore` demuestran que la sustitución de
adaptadores funciona. Lo que hay que corregir es concreto:

| # | Fuga | Dónde | Por qué importa | Corrección |
|---|---|---|---|---|
| A1 | Los puertos de entrada dependen de DTOs de la capa de aplicación | `HoldingUseCase` importa `CreateHoldingRequest` (con anotaciones `jakarta.validation`) | El dominio queda acoplado a la forma del payload HTTP y a una librería de validación | Los puertos reciben **comandos de dominio** (`CreateHoldingCommand`); el controlador mapea DTO → comando |
| A2 | Un servicio de aplicación depende de otro **puerto de entrada** | `WealthQueryService` inyecta `HoldingUseCase`, `PlatformUseCase`, `HistoryUseCase` | Los puertos de entrada son la fachada para el *exterior*; usarlos entre servicios crea grafos de casos de uso y bloquea el testing | `WealthQueryService` depende de puertos de **salida** (`HoldingRepository`, `PlatformRepository`, `SnapshotRepository`) |
| A3 | Datos de dominio hardcodeados en servicios | `PlatformService`, `HistoryService`, `KvHoldingRepository.seed()` | La aplicación no puede escribir lo que devuelve; el frontend sí puede | Persistencia real; los datos de demo salen del código de producción |
| A4 | Dinero como `Double` | `Holding.value`, todos los DTOs | Errores de redondeo acumulativos en sumas y porcentajes | `Money` (`BigDecimal`, escala 2, `HALF_UP`) |
| A5 | Agregación en memoria | `WealthQueryService.getSummary()` hace `findAll().groupBy{}` | O(n) transferido y deserializado por cada carga del dashboard | Puerto de lectura con `SUM ... GROUP BY` en SQL |
| A6 | `@ExceptionHandler(Exception::class)` devuelve `ex.message` al cliente | `GlobalExceptionHandler` | Filtración de información (SQL, rutas, nombres de clase) | `ProblemDetail` genérico + `traceId`; el detalle sólo al log |
| A7 | `PUT` con todos los campos opcionales | `HoldingController.updateHolding` | `PUT` es reemplazo total por definición; el body es un parche | `PATCH` con `JsonNullable`/campos opcionales explícitos |
| A8 | `scan(prefix)` traduce a `LIKE 'prefix%'` sin escapar | `PostgresKvStore` | Un `%` o `_` en la clave cambia el conjunto devuelto; además es un full scan | El KV store desaparece (§2) |
| A9 | Sin capa de seguridad | todo el backend | Cualquiera lee y borra el patrimonio de cualquiera | Resource Server OAuth2 con JWT de Supabase |
| A10 | `userId` viaja en el body | `CreateTaskRequest` | Suplantación trivial de identidad | La identidad viene del JWT, punto |

---

## 2. Decisión estructural: el KV store se retira

`KvStore` fue un atajo deliberado y está documentado como tal en el código
(`// ponytail: local/test KVS is one JSON file on disk…`). Con multi-tenancy deja de servir:

- Consultar "los holdings del usuario X" obliga a escanear **todas** las filas de **todos** los
  usuarios y filtrar en la JVM. No es indexable.
- Los agregados (`SUM`, `GROUP BY`) tienen que hacerse en memoria (A5).
- No hay integridad referencial entre holdings y plataformas.
- Un `LIKE` sin escapar sobre la clave es un bug latente (A8).

**Decisión:** tablas reales con Flyway (`data-model.md`). `KvStore`, `FileKvStore`, `PostgresKvStore`,
`KvHoldingRepository` y `KvTaskRepository` **se borran**; la tabla `kv_store` se elimina en una
migración. Es la deuda que este spec paga, no una capa que se agrega.

Para el desarrollo local sin instalar nada: Postgres via Docker Compose o el
`@ServiceConnection` de Testcontainers en el perfil `dev` (§7.3).

---

## 3. Arquitectura objetivo

### 3.1 Estructura de paquetes

```
com.base.wealth
├─ domain/                            ← Kotlin puro. Cero frameworks.
│  ├─ model/
│  │   ├─ UserId.kt                   value class sobre UUID
│  │   ├─ Money.kt                    BigDecimal escala 2 + operaciones seguras
│  │   ├─ Holding.kt                  entidad + invariantes + factory
│  │   ├─ Platform.kt / PlatformName.kt / PlatformType.kt
│  │   ├─ AssetClass.kt               value class con normalización (trim/colapso)
│  │   ├─ NetWorthSnapshot.kt
│  │   └─ projection/
│  │       ├─ ProjectionParams.kt     invariantes de rango
│  │       ├─ ProjectionSeries.kt
│  │       └─ Milestone.kt            + MilestoneStatus
│  ├─ service/                        ← lógica de dominio sin estado ni I/O
│  │   ├─ CompoundInterestCalculator.kt
│  │   └─ LiquidityPolicy.kt
│  ├─ port/inbound/                   ← casos de uso (comandos y queries de dominio)
│  │   ├─ ManageHoldingsUseCase.kt
│  │   ├─ ManagePlatformsUseCase.kt
│  │   ├─ QueryWealthUseCase.kt
│  │   ├─ TrackHistoryUseCase.kt
│  │   └─ ProjectWealthUseCase.kt
│  ├─ port/outbound/
│  │   ├─ HoldingRepository.kt
│  │   ├─ PlatformRepository.kt
│  │   ├─ SnapshotRepository.kt
│  │   ├─ WealthAggregationPort.kt    ← lecturas agregadas (SQL), separado del write model
│  │   ├─ FxRatePort.kt
│  │   └─ ClockPort.kt                ← el tiempo es I/O; se inyecta para poder testearlo
│  └─ error/                          ← excepciones de dominio, sin HTTP
│      └─ DomainErrors.kt             HoldingNotFound, PlatformInUse, DuplicatePlatform, …
│
├─ application/                       ← orquestación, transacciones. Conoce el dominio.
│  ├─ HoldingApplicationService.kt
│  ├─ PlatformApplicationService.kt
│  ├─ WealthSummaryService.kt
│  ├─ HistoryService.kt
│  └─ ProjectionService.kt
│
└─ infrastructure/
   ├─ adapter/inbound/web/
   │   ├─ HoldingController.kt  PlatformController.kt  WealthController.kt
   │   ├─ AssetClassController.kt  HealthController.kt
   │   ├─ dto/                       ← request/response HTTP + jakarta.validation
   │   ├─ mapper/                    ← DTO ⇄ comando/dominio (funciones de extensión)
   │   └─ error/ApiExceptionHandler.kt   ← dominio → ProblemDetail (RFC 9457)
   ├─ adapter/inbound/security/
   │   ├─ SecurityConfig.kt
   │   ├─ SupabaseJwtDecoderFactory.kt
   │   └─ CurrentUserArgumentResolver.kt ← JWT.sub → UserId, inyectado como parámetro
   ├─ adapter/outbound/persistence/
   │   ├─ JdbcHoldingRepository.kt  JdbcPlatformRepository.kt  JdbcSnapshotRepository.kt
   │   ├─ JdbcWealthAggregationAdapter.kt
   │   └─ rowmapper/
   ├─ adapter/outbound/fx/
   │   └─ FixedFxRateAdapter.kt      (+ HttpFxRateAdapter cuando exista la vista que lo pida)
   └─ config/  ← OpenApiConfig, WebConfig(CORS), WealthProperties, ClockConfig
```

Regla que hace esto verificable: **`domain/` no importa nada fuera de `kotlin.*`, `java.*` y
`com.base.wealth.domain.*`.** Un test de arquitectura lo comprueba (§8.2), no la buena voluntad.

### 3.2 Flujo de un request

```
HTTP POST /api/v1/holdings
  │  Bearer <supabase JWT>
  ▼
[SecurityFilterChain]  valida firma/iss/aud/exp contra el JWKS de Supabase
  ▼
[HoldingController]    @Valid CreateHoldingRequest      ← validación de forma (formato)
  │                    CurrentUser → UserId             ← identidad, del token
  │                    request.toCommand(userId)        ← mapper
  ▼
[ManageHoldingsUseCase]  ← puerto de entrada (interfaz en domain/)
  ▼
[HoldingApplicationService]  @Transactional             ← límite transaccional
  │   1. Holding.create(...)                            ← invariantes de dominio (semántica)
  │   2. platformRepository.ensureExists(userId, name)  ← alta implícita (CA-02.2)
  │   3. holdingRepository.save(userId, holding)
  ▼
[JdbcHoldingRepository]  INSERT … WHERE user_id = ?
  ▼
201 + Location + HoldingResponse
```

Dos niveles de validación, a propósito y sin solaparse:

- **Forma** (`jakarta.validation` en el DTO, en `infrastructure`): "`value` es un número, `name` no
  está vacío, no supera 120 caracteres". Barata, declarativa, devuelve `400` con lista de campos.
- **Semántica** (constructores/factories en `domain`): "un `Money` no puede ser negativo",
  "`years ∈ [1,50]`". Sobrevive aunque mañana el caso de uso lo dispare un job o un test, no un
  controlador. La duplicación aparente es la única defensa contra el día en que alguien invoque el
  caso de uso sin pasar por HTTP.

### 3.3 Identidad como parámetro explícito

```kotlin
// domain/port/inbound/ManageHoldingsUseCase.kt
interface ManageHoldingsUseCase {
    fun list(userId: UserId, filter: HoldingFilter): List<Holding>
    fun create(command: CreateHoldingCommand): Holding      // el command lleva el userId
    fun patch(command: PatchHoldingCommand): Holding
    fun delete(userId: UserId, holdingId: HoldingId)
}
```

No hay forma de llamar a estos métodos sin decir de quién son los datos: el compilador lo impide.
Es lo que hace que el riesgo R2 sea estructural y no una cuestión de disciplina. `SecurityContextHolder`
(un `ThreadLocal`) sólo se lee **una vez**, en el borde HTTP, dentro de `CurrentUserArgumentResolver`.

```kotlin
// infrastructure/adapter/inbound/security/CurrentUserArgumentResolver.kt
@Target(VALUE_PARAMETER) annotation class CurrentUser

@Component
class CurrentUserArgumentResolver : HandlerMethodArgumentResolver {
    override fun supportsParameter(p: MethodParameter) =
        p.hasParameterAnnotation(CurrentUser::class.java) && p.parameterType == UserId::class.java

    override fun resolveArgument(p: MethodParameter, /*…*/): UserId {
        val jwt = SecurityContextHolder.getContext().authentication.principal as Jwt
        return UserId(UUID.fromString(jwt.subject))   // 'sub' de Supabase = auth.users.id
    }
}

// uso
@GetMapping
fun list(@CurrentUser userId: UserId, filter: HoldingFilterParams) = …
```

---

## 4. Patrones de diseño

### 4.1 Aplicados (y por qué se ganan el lugar)

| Patrón | Dónde | Qué problema resuelve concretamente |
|---|---|---|
| **Ports & Adapters (Hexagonal)** | toda la app | Ya es la arquitectura del proyecto. Este plan **le saca las fugas** (A1, A2), no la reemplaza. |
| **Repository** | `HoldingRepository`, `PlatformRepository`, `SnapshotRepository` | El dominio habla de colecciones de entidades; JDBC, Flyway y el pooler quedan del otro lado del puerto. |
| **Value Object** | `Money`, `UserId`, `HoldingId`, `AssetClass`, `PlatformName` | Elimina la clase entera de bugs "pasé los argumentos al revés" y centraliza normalización (trim, case) y redondeo. `@JvmInline value class` ⇒ costo cero en runtime. |
| **Factory Method** (estático, en el companion) | `Holding.create()`, `Money.ofUsd()`, `ProjectionParams.of()` | Un objeto de dominio no puede existir en estado inválido: el único camino de construcción valida. |
| **CQS ligera** (separar el modelo de lectura del de escritura) | `WealthAggregationPort` vs `HoldingRepository` | El dashboard necesita `SUM/GROUP BY`, no entidades. Separarlos permite SQL agregado (NFR-2) sin contaminar el modelo de escritura. **Sin** event store, sin bases separadas, sin sincronización: son dos consultas a la misma tabla. |
| **Strategy** | `FxRatePort` → `FixedFxRateAdapter` / (futuro) `HttpFxRateAdapter` | El origen del tipo de cambio va a cambiar (D4). El puerto tiene 1 implementación hoy **y un fake en tests** — cumple el principio 5. |
| **Adapter** | `JdbcHoldingRepository`, `SupabaseJwtDecoder` | Traducir entre el vocabulario del dominio y el de la tecnología, en un solo archivo por tecnología. |
| **Command** (objeto-parámetro) | `CreateHoldingCommand`, `PatchHoldingCommand` | Firmas estables (agregar un campo no rompe llamadores), y el `userId` viaja pegado a la intención. |
| **Specification-lite** | `HoldingFilter(assetClass?, platform?)` | Un data class que el adaptador traduce a `WHERE` dinámico. **No** el patrón Specification completo con `and/or/not` componibles: hay dos filtros, no un motor de queries. |
| **Null Object** | `FxRate.UNAVAILABLE`, `YtdGrowth.NO_BASELINE` | El caso "no hay dato" es un valor legítimo del dominio (CA-05.7), no un `null` que cada llamador tiene que recordar chequear. |

### 4.2 Rechazados (y por qué)

Esta sección es tan parte del diseño como la anterior. Sin ella, "aplicar patrones" degenera en
agregarlos todos.

| Patrón | Veredicto | Motivo |
|---|---|---|
| **CQRS completo** (write model + read model + proyecciones + sincronización) | ❌ | Son 5 tablas y un usuario por cuenta. La lectura agregada es un `GROUP BY`, no una proyección eventual. |
| **Event Sourcing** | ❌ | Nadie pidió auditoría ni time-travel. Los snapshots ya cubren la única necesidad histórica real. |
| **Outbox / mensajería** | ❌ | No hay consumidores. No hay side effects fuera de la propia DB. |
| **Aggregate Root + Domain Events con `ApplicationEventPublisher`** | ❌ | Un `Holding` no coordina invariantes con otras entidades. Sería ceremonia sin consistencia que proteger. |
| **`Either`/`Result` (Arrow) en lugar de excepciones** | ❌ | Agrega una dependencia y contamina cada firma para modelar errores que en el 100 % de los casos terminan en un `ProblemDetail`. `@RestControllerAdvice` ya hace ese mapeo en un solo lugar. |
| **MapStruct / ModelMapper** | ❌ | Los mapeos son funciones de extensión de 5 líneas. Un procesador de anotaciones para eso es peor: falla en compilación, se debuggea peor y agrega tiempo de build. |
| **Abstract Factory / Builder** para entidades | ❌ | Los `data class` de Kotlin ya tienen argumentos nombrados, valores por defecto y `copy()`. Un builder es reimplementar el lenguaje. |
| **Decorator para caché del FX** | ❌ | `@Cacheable` con el `CacheManager` de Spring Boot, que ya está en el classpath, resuelve lo mismo sin una clase nueva. Si algún día el caché tiene que ser framework-free, ahí se escribe el decorator. |
| **Unit of Work explícito** | ❌ | `@Transactional` **es** el Unit of Work, provisto por el framework. |
| **Interfaz genérica `BaseRepository<T, ID>` / servicio genérico** | ❌ | Herencia por conveniencia sintáctica. Acopla los tres repositorios entre sí para ahorrar tres firmas. |
| **Mapper de dominio a JPA + JPA/Hibernate** | ❌ | Con 5 tablas, sin grafos de objetos ni lazy loading, `JdbcClient` (Spring 6.1) es más simple, el SQL es explícito y no hay N+1 posibles. La única razón para JPA sería un modelo relacional profundo, y no lo hay. |

---

## 5. Seguridad

### 5.1 Verificación del JWT de Supabase

Supabase Auth emite el JWT que el frontend ya tiene en la sesión. El backend lo valida como
**OAuth2 Resource Server**; no emite tokens propios ni guarda sesiones.

```yaml
spring:
  security:
    oauth2:
      resourceserver:
        jwt:
          jwk-set-uri: ${SUPABASE_URL}/auth/v1/.well-known/jwks.json
wealth:
  auth:
    issuer: ${SUPABASE_URL}/auth/v1
    audience: authenticated
```

```kotlin
@Bean
fun jwtDecoder(props: WealthProperties): JwtDecoder =
    NimbusJwtDecoder.withJwkSetUri(props.auth.jwkSetUri).build().apply {
        setJwtValidator(
            DelegatingOAuth2TokenValidator(
                JwtValidators.createDefaultWithIssuer(props.auth.issuer), // exp, nbf, iss
                JwtClaimValidator<List<String>>("aud") { props.auth.audience in it },
            )
        )
    }
```

- Rotación de claves: automática vía JWKS (Nimbus cachea y refresca).
- Proyectos con el secreto HS256 legacy: `NimbusJwtDecoder.withSecretKey(...)` detrás de la misma
  `@Bean`, seleccionada por propiedad. Un solo punto de cambio.
- **Nunca** se loguea el token ni sus claims completos. Sólo `sub`, y sólo en el MDC.

### 5.2 Cadena de filtros

```kotlin
@Bean
fun filterChain(http: HttpSecurity): SecurityFilterChain = http
    .csrf { it.disable() }                        // API stateless con Bearer token: no aplica CSRF
    .sessionManagement { it.sessionCreationPolicy(STATELESS) }
    .cors { }                                     // usa el CorsConfigurationSource de WebConfig
    .authorizeHttpRequests {
        it.requestMatchers("/api/v1/health", "/actuator/health/**").permitAll()
          .requestMatchers("/v3/api-docs/**", "/swagger-ui/**", "/swagger-ui.html").permitAll()
          .anyRequest().authenticated()
    }
    .oauth2ResourceServer { it.jwt { } }
    .exceptionHandling {                          // 401/403 también como problem+json
        it.authenticationEntryPoint(ProblemDetailAuthEntryPoint())
          .accessDeniedHandler(ProblemDetailAccessDeniedHandler())
    }
    .headers { it.frameOptions { f -> f.deny() }.contentTypeOptions { } }
    .build()
```

Swagger UI queda público en `dev` y **cerrado en `prod`** (`springdoc.swagger-ui.enabled: false`),
porque `docs/api/openapi.json` versionado ya cubre la necesidad de consultarlo.

### 5.3 Autorización a nivel de dato

No hay roles ni permisos: hay **propiedad**. La regla es una sola y se aplica en el adaptador de
persistencia, donde no se puede saltear:

```kotlin
override fun findById(userId: UserId, id: HoldingId): Holding? = jdbcClient
    .sql("SELECT * FROM holdings WHERE id = :id AND user_id = :userId")
    .param("id", id.value).param("userId", userId.value)
    .query(HoldingRowMapper).optional().orElse(null)
```

Un recurso ajeno devuelve `null` → el caso de uso lanza `HoldingNotFound` → `404` (CA-01.4). Nunca
`403`: un `403` confirmaría que el recurso existe.

### 5.4 RLS en Postgres (defensa en profundidad, D5)

El backend conecta con un rol privilegiado, así que RLS no lo limita. Se activa igual sobre las
tablas nuevas para que **el anon key de Supabase / PostgREST no pueda tocarlas** aunque alguien exponga
esa ruta mañana. Políticas `USING (auth.uid() = user_id)` en las cuatro tablas (`data-model.md` §4).

Endurecimiento opcional, **no** en v1: conectar como rol `authenticated` y emitir
`SET LOCAL request.jwt.claims` por transacción para que RLS sea el control primario. Cuesta un
`TransactionSynchronization` propio y complica el pooling; se documenta y se deja anotado.

### 5.5 Otros controles

- **CORS**: orígenes explícitos por config (`FRONTEND_ORIGIN`). Con `allowCredentials(true)` el patrón
  `https://*.vercel.app` que hay hoy es demasiado ancho — se restringe al dominio de despliegue real.
  (Con Bearer tokens `allowCredentials` ni siquiera hace falta: se pone en `false`.)
- **Límites de payload**: `spring.servlet.multipart` deshabilitado; `server.max-http-request-header-size` acotado.
- **Rate limiting**: no en v1. Anotado como el primer candidato si la API se expone públicamente
  (Bucket4j sobre `userId`, o directamente en el borde de la plataforma de despliegue).
- **Dependencias**: `gradle dependencyCheckAnalyze` (OWASP) + Dependabot semanal.

---

## 6. Contrato HTTP

### 6.1 Convenciones

- Base: `/api/v1`. La versión se sube sólo ante cambios incompatibles.
- Recursos en plural y en inglés (consistente con lo existente).
- `PATCH` para actualizaciones parciales; `PUT` sólo si alguna vez hay reemplazo total.
- `201` + `Location` en creaciones; `204` sin cuerpo en borrados; `409` en conflictos de unicidad
  y de integridad referencial.
- Fechas ISO-8601 UTC (`Instant`). Meses como `YYYY-MM`. **Cero strings pre-formateados para la UI.**
- Importes: número JSON con 2 decimales (`BigDecimal` serializado con `writeBigDecimalAsPlain`).
  Se mantiene numérico —y no string— para no forzar cambios en el frontend; el rango de un portafolio
  personal está muy por debajo del entero seguro de JavaScript (2^53).

El contrato completo está en [`contracts/openapi.yaml`](contracts/openapi.yaml). Ese archivo es
**la fuente de verdad del diseño**; `backend/docs/api/openapi.json` sigue siendo el reflejo generado
desde el código, y un test compara ambos (§8.4).

### 6.2 Errores — RFC 9457 (`ProblemDetail`, ya incluido en Spring 6)

```json
{
  "type": "https://base.wealth/errors/validation",
  "title": "Validation failed",
  "status": 400,
  "detail": "The request body has 2 invalid fields",
  "instance": "/api/v1/holdings",
  "traceId": "0af7651916cd43dd8448eb211c80319c",
  "errors": [
    { "field": "value", "message": "must be greater than 0" },
    { "field": "name",  "message": "must not be blank" }
  ]
}
```

```kotlin
@RestControllerAdvice
class ApiExceptionHandler(private val tracer: Tracer) {

    @ExceptionHandler(HoldingNotFoundException::class, PlatformNotFoundException::class)
    fun notFound(ex: DomainException) = problem(NOT_FOUND, "not-found", ex.message)

    @ExceptionHandler(DuplicatePlatformException::class, PlatformInUseException::class)
    fun conflict(ex: DomainException) = problem(CONFLICT, "conflict", ex.message)

    @ExceptionHandler(MethodArgumentNotValidException::class)
    fun invalid(ex: MethodArgumentNotValidException) = problem(BAD_REQUEST, "validation", …)
        .apply { setProperty("errors", ex.bindingResult.fieldErrors.map { FieldError(it.field, it.defaultMessage) }) }

    @ExceptionHandler(Exception::class)
    fun unexpected(ex: Exception): ProblemDetail {
        log.error("Unhandled exception", ex)                       // el detalle va al log…
        return problem(INTERNAL_SERVER_ERROR, "internal",
                       "An unexpected error occurred. Quote the traceId when reporting it.")
    }                                                              // …nunca al cliente (A6)
}
```

Los mensajes de dominio son **de negocio y en inglés** ("Platform 'Binance' still has 3 holdings"),
no fugas técnicas. Los mensajes de validación `jakarta` actuales están en español: se unifican a
inglés en la API y la traducción, si hace falta, es del frontend.

---

## 7. Persistencia y transacciones

### 7.1 Acceso a datos: `JdbcClient`

Spring 6.1 trae `JdbcClient`: API fluida, parámetros nombrados, sin el boilerplate de `JdbcTemplate`
y sin la maquinaria de JPA. Con 5 tablas planas y sin grafos de objetos, es la elección correcta
(§4.2). El SQL vive en el adaptador, visible, testeable con Testcontainers.

### 7.2 Transacciones

- `@Transactional` **sólo** en la capa de aplicación. Nunca en controladores (transacción abierta
  durante la serialización) ni en repositorios (fragmenta la unidad de trabajo).
- `@Transactional(readOnly = true)` en las queries: habilita optimizaciones del driver y documenta la intención.
- Alta implícita de plataforma (CA-02.2) y creación del holding: **una sola transacción**.
  `INSERT … ON CONFLICT (user_id, lower(name)) DO NOTHING` evita la carrera entre dos altas simultáneas.
- Snapshots (CA-06.1): la lectura del neto y la escritura del snapshot van en la misma transacción,
  para que el importe guardado sea consistente con lo que había al capturar.

### 7.3 Perfiles

| Perfil | Base de datos | Uso |
|---|---|---|
| `dev` (default) | Postgres local (Docker Compose incluido en el repo) | Desarrollo |
| `test` | Testcontainers `postgres:16-alpine` con `@ServiceConnection` | Tests de integración |
| `prod` | Supabase, vía Session Pooler (IPv4) | Despliegue |

Flyway corre en los tres. El esquema es idéntico en todos: se elimina la divergencia
archivo-JSON-vs-Postgres que hay hoy, que hace que los bugs de persistencia sólo aparezcan en prod.

---

## 8. Calidad de código y gates

### 8.1 Análisis estático

| Herramienta | Config | Gate |
|---|---|---|
| **ktlint** (vía Spotless) | `.editorconfig`, estilo oficial de Kotlin | `spotlessCheck` falla el build |
| **detekt** | complejidad ciclomática ≤ 10, longitud de función ≤ 40 líneas, `MaxLineLength 120`, `ForbiddenMethodCall` para `println` | Build falla con issues de severidad `error` |
| **Konsist** | Reglas de arquitectura (§8.2) | Test que falla el build |
| **JaCoCo** | ≥ 80 % en `domain`/`application`, ≥ 60 % global | `jacocoTestCoverageVerification` |
| **OWASP dependency-check** | CVSS ≥ 7 falla | Job semanal en CI |
| **gitleaks** | pre-commit + CI | Falla el push |

Compilador: `-Xjsr305=strict` (ya está) + `allWarningsAsErrors = true`. Un warning de Kotlin es un
bug que todavía no se disparó.

### 8.2 Tests de arquitectura (Konsist — la regla que se autocontrola)

```kotlin
@Test
fun `domain no depende de frameworks`() {
    Konsist.scopeFromPackage("com.base.wealth.domain..")
        .files
        .assertFalse { file ->
            file.imports.any {
                it.name.startsWith("org.springframework") ||
                it.name.startsWith("jakarta.")           ||
                it.name.startsWith("com.fasterxml")      ||
                it.name.startsWith("com.base.wealth.infrastructure")
            }
        }
}

@Test fun `application no importa infrastructure`()          { … }
@Test fun `los controladores dependen de puertos de entrada, no de servicios concretos`() { … }
@Test fun `ninguna firma de repositorio omite UserId`()       { … }   // NFR-3
@Test fun `ningun tipo de dominio usa Double o Float`()        { … }   // NFR-8
@Test fun `toda clase de servicio es final y con constructor injection`() { … }
```

### 8.3 Pirámide de tests

| Nivel | Alcance | Herramientas | Qué cubre |
|---|---|---|---|
| **Unitarios de dominio** | sin mocks, sin Spring | JUnit 5 + `kotlin.test` | `Money` (redondeo, suma, porcentaje sobre 0), `CompoundInterestCalculator` (r=0, r>0, años=1, hitos justo en el borde), `LiquidityPolicy`, normalización de `AssetClass` |
| **Unitarios de aplicación** | puertos mockeados | MockK | Alta implícita de plataforma, borrado inexistente → excepción, snapshot con 0 holdings |
| **Adaptadores de persistencia** | DB real | Testcontainers + Flyway | SQL, índices, `ON CONFLICT`, agregaciones, **aislamiento entre usuarios** |
| **Web slice** | sin DB | `@WebMvcTest` + `springmockk` + `spring-security-test` (`.with(jwt().jwt { it.subject(uid) })`) | Códigos de estado, forma del JSON, `problem+json`, `401` sin token |
| **Contrato** | app completa | swagger-request-validator sobre `contracts/openapi.yaml` | Que ninguna respuesta se salga del contrato publicado |
| **Integración e2e** | app + DB | `@SpringBootTest(webEnvironment=RANDOM_PORT)` | Los flujos de HU-02, HU-05, HU-06 de punta a punta |

Casos que **siempre** tienen test, porque rompen en silencio: portafolio vacío (CA-05.4), un solo
holding, valores con centavos que no cierran (`33.33 × 3`), `yieldPct = 0`, hito ya alcanzado, hito
inalcanzable, y el aislamiento entre dos usuarios en **cada** endpoint.

### 8.4 CI (GitHub Actions)

```
build → spotlessCheck → detekt → test (unit) → test (integration, Testcontainers)
      → jacocoTestCoverageVerification → konsist → generateOpenApiDocs
      → diff contracts/openapi.yaml vs docs/api/openapi.json  (falla si divergen)
```

El hook `.githooks/pre-commit` que ya regenera `openapi.json` se mantiene; CI lo verifica de nuevo,
porque un hook local es opcional por diseño (`git config core.hooksPath`).

### 8.5 Observabilidad

- **Logs estructurados** (JSON en `prod`) con MDC: `traceId`, `userId`, `method`, `path`, `status`,
  `durationMs`. **Nunca** email, token ni importes.
- **Micrometer + Actuator**: `/actuator/health/liveness`, `/actuator/health/readiness` (con check de
  DB), `/actuator/metrics`. `/actuator/**` cerrado salvo health.
- Un `OncePerRequestFilter` genera/propaga `X-Request-Id` y lo pone en el MDC y en el `ProblemDetail`.

---

## 9. Impacto en el frontend

Mínimo por diseño: la UI no cambia, cambia de dónde salen los datos.

1. **`lib/api.ts` (nuevo, ~80 líneas)** — `fetch` tipado que adjunta
   `Authorization: Bearer ${session.access_token}`, parsea `problem+json` y expone
   `ApiError { status, title, detail, errors }`. Sin librería de data-fetching nueva: el
   `WealthContext` ya existe y hace de caché.
2. **`WealthContext.tsx`** — los tres `useState` sembrados desde `constants.ts` pasan a cargarse con
   `GET /holdings`, `GET /platforms`, `GET /wealth/snapshots` + `GET /wealth/summary`. Se borran los
   seis helpers de `localStorage` y el `useEffect` de hidratación. Las acciones (`addHolding`,
   `deleteHolding`, `takeSnapshot`) pasan a ser `async` y refrescan el summary tras la mutación.
   Los cálculos derivados (`classDistribution`, `liquidityPct`, `ytdGrowth`, `platformDistribution`)
   dejan de calcularse: llegan del summary. La UI sigue leyendo las mismas propiedades del contexto.
3. **`EstimateView.tsx`** — la serie viene de `GET /wealth/estimate` con debounce de 150 ms sobre los
   sliders; `generateLinePath` se mantiene (es geometría de la vista). Los labels de hito se arman
   desde `status` + `targetMonth`.
4. **`HistoryView.tsx`** — `changePctFromPrevious` viene del backend; la etiqueta del checkpoint se
   formatea desde `capturedAt`.
5. **`lib/calculations.ts`** — se borran `fvYears`, `fvMonths`, `monthsToReach` y
   `dateLabelFromMonths` al cerrar la migración (D1). Quedan `formatCurrency`, `formatPercentage` y
   `generateLinePath`.
6. **`lib/constants.ts`** — se borran `INITIAL_HOLDINGS`, `INITIAL_PLATFORMS`, `INITIAL_HISTORY`.
   Quedan los tokens de color/tag y `MONTH_NAMES`.
7. **Estados de carga y error** — hoy no existen porque nada podía fallar. Hacen falta: skeleton en
   la carga inicial, toast/inline error en las mutaciones, y re-login si llega un `401`.
8. **`.env.local`** — `NEXT_PUBLIC_API_BASE_URL`.
9. **"Skip login (dev)"** (`page.tsx`) — sin sesión no hay token y el backend responde `401`. El botón
   pasa a estar detrás de `NODE_ENV !== 'production'` y muestra datos vacíos, o se elimina.

---

## 10. Fases de entrega

Cada fase deja `main` desplegable. El detalle por tarea está en [`tasks.md`](tasks.md).

| Fase | Contenido | Se sabe que terminó cuando… |
|---|---|---|
| **0 — Cimientos** | Flyway, Docker Compose de Postgres, Testcontainers, perfiles, gates de calidad (ktlint/detekt/JaCoCo/Konsist) en el build | `./gradlew check` corre verde y falla si alguien importa Spring en `domain/` |
| **1 — Seguridad** | Resource Server, `SupabaseJwtDecoder`, `@CurrentUser`, `ProblemDetail` en 401/403 | Todo endpoint sin token da `401`; un test de aislamiento A/B pasa |
| **2 — Dominio y persistencia** | `Money`, value objects, entidades, tablas, repositorios JDBC, borrado del `KvStore` | Los tests de repositorio con Testcontainers pasan; `kv_store` ya no existe |
| **3 — Holdings, platforms, asset classes** | HU-02, HU-03, HU-04 completas | El frontend puede crear/listar/borrar contra el backend real |
| **4 — Summary e historia** | HU-05, HU-06 con agregación SQL, liquidez, YTD, snapshots | El dashboard se pinta 100 % con datos del backend |
| **5 — Proyección** | HU-07, con los casos dorados de paridad | `GET /wealth/estimate` coincide con `calculations.ts` dentro de 0.01 |
| **6 — Corte del frontend** | `lib/api.ts`, `WealthContext` conectado, borrado de `localStorage` y de la matemática duplicada | Limpiar el `localStorage` no pierde ni un dato; los datos aparecen en otro dispositivo |
| **7 — Limpieza** | Decisión sobre `Tasks` (D3), borrado de código muerto, README y OpenAPI al día | `grep -r "localStorage\|INITIAL_HOLDINGS" frontend/src` sale vacío |

---

## 11. Nota de proporción

Este backend sirve a **un usuario por cuenta, con decenas de holdings**. La arquitectura hexagonal, los
value objects y los gates de calidad valen la pena porque el código va a vivir años y porque el
aislamiento entre usuarios y la exactitud del dinero **no se pueden aflojar**. El resto del catálogo
—CQRS, event sourcing, outbox, `Either`, MapStruct, JPA— quedó fuera a propósito (§4.2), y esa lista
es parte del diseño: sin ella, "aplicar buenas prácticas" termina siendo cinco capas de indirección
para hacer un `SELECT`.

Si algo de este plan se siente pesado al implementarlo, la pregunta correcta no es "¿cómo lo hago
más elegante?" sino "¿qué requisito de `spec.md` se rompe si lo saco?". Si la respuesta es "ninguno",
se saca.
