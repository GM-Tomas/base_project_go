# Modelo de datos 001

Complemento de [`spec.md`](spec.md) y [`plan.md`](plan.md).

---

## 1. Modelo de dominio

```
                    ┌──────────────┐
                    │   UserId     │  (= auth.users.id de Supabase; no lo poseemos)
                    └──────┬───────┘
           ┌───────────────┼──────────────────┬─────────────────────┐
           │               │                  │                     │
     ┌─────▼─────┐   ┌─────▼──────┐    ┌──────▼────────┐     ┌──────▼──────┐
     │  Holding  │──▶│  Platform  │    │ NetWorth      │     │    Task     │
     │           │   │            │    │ Snapshot      │     │ (candidato  │
     └───────────┘   └────────────┘    └───────────────┘     │  a borrado) │
           │                                                  └─────────────┘
           └──▶ AssetClass  (derivada de los holdings, sin tabla propia)
```

### 1.1 Value objects

| Tipo | Representación | Invariantes | Notas |
|---|---|---|---|
| `UserId` | `@JvmInline value class UserId(val value: UUID)` | — | Del claim `sub` del JWT. Costo cero en runtime. |
| `HoldingId` | `value class(UUID)` | — | Generado por el servidor (`UUID.randomUUID()` en el dominio, no en la DB: la entidad se construye completa antes de persistir y no hace falta un round-trip para conocer su id). |
| `Money` | `BigDecimal` escala 2, `RoundingMode.HALF_UP`, moneda `USD` | `>= 0` | `plus`, `times(BigDecimal)`, `percentOf(total): BigDecimal?` (`null` si `total == 0`, evitando la división por cero de CA-05.4). |
| `AssetClass` | `value class(String)` | no vacío tras `trim`, ≤ 60 chars | Normaliza: `trim()` + colapso de espacios internos. Comparación **case-sensitive** (`"Crypto"` ≠ `"crypto"`), consistente con el filtro actual del frontend. |
| `PlatformName` | `value class(String)` | no vacío tras `trim`, ≤ 120 chars | Unicidad **case-insensitive** por usuario (`lower(name)`), para que no convivan `Binance` y `binance`. |
| `PlatformType` | `value class(String)` | no vacío, ≤ 40 chars | Texto libre (el frontend ya permite altas). Default `"Other"`. |
| `FxRate` | `value(rate: BigDecimal, asOf: Instant)` | `rate > 0` | `FxRate.UNAVAILABLE` como Null Object. |

Por qué `value class` y no `typealias` (que es lo que hay hoy en `Enums.kt`): un `typealias
AssetClass = String` no da ninguna garantía — `holding.copy(cls = holding.platform)` compila
perfectamente. Un `value class` lo rechaza en tiempo de compilación y sigue siendo un `String`
en el bytecode.

### 1.2 Entidades

```kotlin
// domain/model/Holding.kt  — Kotlin puro
data class Holding(
    val id: HoldingId,
    val userId: UserId,
    val name: String,
    val assetClass: AssetClass,
    val platform: PlatformName,
    val value: Money,
    val createdAt: Instant,
    val updatedAt: Instant,
) {
    init {
        require(name.isNotBlank()) { "Holding name must not be blank" }
        require(name.length <= 120) { "Holding name must not exceed 120 characters" }
    }

    fun patch(name: String?, assetClass: AssetClass?, platform: PlatformName?,
              value: Money?, now: Instant): Holding =
        copy(
            name = name?.trim() ?: this.name,
            assetClass = assetClass ?: this.assetClass,
            platform = platform ?: this.platform,
            value = value ?: this.value,
            updatedAt = now,
        )

    companion object {
        fun create(userId: UserId, name: String, assetClass: AssetClass,
                   platform: PlatformName, value: Money, now: Instant) =
            Holding(HoldingId(UUID.randomUUID()), userId, name.trim(),
                    assetClass, platform, value, now, now)
    }
}
```

```kotlin
data class Platform(
    val userId: UserId,
    val name: PlatformName,
    val type: PlatformType,
    val createdAt: Instant,
)

data class NetWorthSnapshot(
    val id: SnapshotId,
    val userId: UserId,
    val capturedAt: Instant,
    val totalValue: Money,
)
```

### 1.3 Objetos de proyección (dominio, sin persistencia)

```kotlin
data class ProjectionParams private constructor(
    val principal: Money, val monthlyContribution: Money,
    val annualYieldPct: BigDecimal, val years: Int, val milestones: List<Money>,
) {
    companion object {
        const val MAX_YEARS = 50
        const val MAX_MILESTONES = 5
        fun of(principal: Money, contribution: Money, yieldPct: BigDecimal,
               years: Int, milestones: List<Money>): ProjectionParams {
            require(years in 1..MAX_YEARS)                 { "years must be between 1 and $MAX_YEARS" }
            require(yieldPct >= ZERO && yieldPct <= 100.toBigDecimal()) { "yieldPct must be between 0 and 100" }
            require(milestones.size <= MAX_MILESTONES)     { "at most $MAX_MILESTONES milestones" }
            return ProjectionParams(principal, contribution, yieldPct, years, milestones.sorted())
        }
    }
}

enum class MilestoneStatus { ACHIEVED, REACHABLE, OUT_OF_HORIZON }

data class Milestone(
    val amount: Money, val status: MilestoneStatus,
    val monthsRequired: Int?, val targetMonth: YearMonth?,
)
```

### 1.4 Modelos de lectura (no son entidades: no se persisten, no tienen identidad)

```kotlin
data class WealthSummary(
    val netWorth: Money, val fx: FxRate, val holdingsCount: Int,
    val ytd: YtdGrowth, val liquidity: LiquidityBreakdown,
    val byAssetClass: List<AssetClassBucket>, val byPlatform: List<PlatformBucket>,
)
data class AssetClassBucket(val assetClass: AssetClass, val value: Money,
                            val pct: BigDecimal, val count: Int)
data class PlatformBucket(val name: PlatformName, val type: PlatformType,
                          val value: Money, val pct: BigDecimal, val count: Int)
data class LiquidityBreakdown(val liquidPct: BigDecimal, val illiquidPct: BigDecimal)
sealed interface YtdGrowth {
    data class From(val baselineValue: Money, val baselineAt: Instant,
                    val growthPct: BigDecimal) : YtdGrowth
    data object NoBaseline : YtdGrowth                  // CA-05.7 (Null Object)
}
```

---

## 2. Esquema relacional

`user_id` referencia `auth.users(id)`, el esquema propio de Supabase Auth. `ON DELETE CASCADE`:
si se borra la cuenta, se borran sus datos — es lo que ya hace `tasks` y lo que corresponde.

### V2 — tablas de patrimonio

```sql
-- supabase/migrations/V2__wealth_tables.sql
CREATE TABLE platforms (
    user_id    UUID        NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    name       TEXT        NOT NULL,
    type       TEXT        NOT NULL DEFAULT 'Other',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, name),
    CONSTRAINT platforms_name_len CHECK (char_length(btrim(name)) BETWEEN 1 AND 120),
    CONSTRAINT platforms_type_len CHECK (char_length(btrim(type)) BETWEEN 1 AND 40)
);

-- Unicidad case-insensitive: 'Binance' y 'binance' no pueden coexistir para el mismo usuario.
CREATE UNIQUE INDEX platforms_user_lower_name_uk ON platforms (user_id, lower(name));

CREATE TABLE holdings (
    id            UUID          PRIMARY KEY,
    user_id       UUID          NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    name          TEXT          NOT NULL,
    asset_class   TEXT          NOT NULL,
    platform_name TEXT          NOT NULL,
    value_usd     NUMERIC(20,2) NOT NULL,
    created_at    TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT holdings_value_non_negative CHECK (value_usd >= 0),
    CONSTRAINT holdings_name_len   CHECK (char_length(btrim(name))        BETWEEN 1 AND 120),
    CONSTRAINT holdings_class_len  CHECK (char_length(btrim(asset_class)) BETWEEN 1 AND 60),
    -- La FK compuesta garantiza que un holding no puede apuntar a la plataforma de OTRO usuario.
    CONSTRAINT holdings_platform_fk FOREIGN KEY (user_id, platform_name)
        REFERENCES platforms (user_id, name) ON UPDATE CASCADE ON DELETE RESTRICT
);

CREATE INDEX holdings_user_created_idx  ON holdings (user_id, created_at);
CREATE INDEX holdings_user_class_idx    ON holdings (user_id, asset_class);
CREATE INDEX holdings_user_platform_idx ON holdings (user_id, platform_name);

CREATE TABLE net_worth_snapshots (
    id              UUID          PRIMARY KEY,
    user_id         UUID          NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    captured_at     TIMESTAMPTZ   NOT NULL DEFAULT now(),
    total_value_usd NUMERIC(20,2) NOT NULL,
    CONSTRAINT snapshots_value_non_negative CHECK (total_value_usd >= 0),
    -- CA-06.4: un snapshot por segundo por usuario (protege contra doble click).
    CONSTRAINT snapshots_user_instant_uk UNIQUE (user_id, captured_at)
);

CREATE INDEX snapshots_user_captured_idx ON net_worth_snapshots (user_id, captured_at);
```

Decisiones que no son obvias:

- **La FK de `holdings` a `platforms` es compuesta `(user_id, name)`**, no `platform_id`. Con la FK
  simple, un bug podría asociar el holding de un usuario a la plataforma de otro; con la compuesta,
  Postgres lo rechaza. Además el nombre **es** el identificador natural que usa el frontend hoy
  (`h.platform === p.name`), así que no hay traducción que mantener.
- **`ON DELETE RESTRICT`** implementa CA-04.3 en la base: borrar una plataforma con holdings falla,
  aunque alguien se saltee la validación de la aplicación.
- **`ON UPDATE CASCADE`** permite renombrar una plataforma arrastrando sus holdings, en una sola
  sentencia y sin ventana de inconsistencia.
- **Sin tabla `asset_classes`** (F9/F10). El frontend sólo permite dar de alta una clase junto con un
  holding, así que "clases disponibles" = `defaults ∪ SELECT DISTINCT asset_class`. Una tabla sería
  estado que sólo puede desincronizarse. Se agrega el día que haya una pantalla de gestión de clases.
- **Sin columna `version`**. La app es de un usuario por cuenta; el bloqueo optimista resolvería una
  colisión que no ocurre. `updated_at` queda para cuando haga falta (`If-Match` con `ETag`).

### V3 — retirar el KV store

```sql
-- supabase/migrations/V3__drop_kv_store.sql
-- El KVS era el atajo de persistencia previo a las tablas reales (plan.md §2).
DROP TABLE IF EXISTS public.kv_store;
```

### V4 — re-scope de tasks (D3)

```sql
-- supabase/migrations/V4__tasks_indexes.sql
CREATE INDEX IF NOT EXISTS tasks_user_created_idx ON public.tasks (user_id, created_at DESC);
```

La tabla `tasks` ya existe con RLS correcta. El cambio es de código: `userId` sale del JWT, no del
body. Si D3 se resuelve como "borrar", esta migración se reemplaza por el `DROP TABLE`.

---

## 3. Consultas clave

El dashboard entero es **una** consulta agregada por dimensión, no `findAll()` + `groupBy` en la JVM
(NFR-2, corrige A5).

```sql
-- Distribución por clase de activo (CA-05.2)
SELECT asset_class,
       SUM(value_usd)                                                  AS total,
       COUNT(*)                                                        AS count,
       ROUND(100 * SUM(value_usd) / NULLIF(SUM(SUM(value_usd)) OVER (), 0), 1) AS pct
FROM holdings
WHERE user_id = :userId
GROUP BY asset_class
ORDER BY total DESC;
```

`NULLIF(..., 0)` es lo que hace que un portafolio vacío devuelva `NULL` en vez de reventar por
división por cero — el lado SQL de CA-05.4. El adaptador mapea ese `NULL` a `BigDecimal.ZERO`.

```sql
-- Distribución por plataforma, incluidas las de saldo 0 (CA-05.3 / F14).
-- LEFT JOIN desde platforms: es exactamente el bug que tiene hoy WealthQueryService,
-- que agrupa desde holdings y por eso pierde las plataformas sin posiciones.
SELECT p.name, p.type,
       COALESCE(SUM(h.value_usd), 0) AS total,
       COUNT(h.id)                   AS count
FROM platforms p
LEFT JOIN holdings h ON h.user_id = p.user_id AND h.platform_name = p.name
WHERE p.user_id = :userId
GROUP BY p.name, p.type
ORDER BY total DESC, p.name;
```

```sql
-- Baseline YTD (CA-05.7): primer snapshot del año en curso.
SELECT total_value_usd, captured_at
FROM net_worth_snapshots
WHERE user_id = :userId AND captured_at >= date_trunc('year', now())
ORDER BY captured_at
LIMIT 1;
-- Si no devuelve filas → el más antiguo; si tampoco → YtdGrowth.NoBaseline.
```

```sql
-- Clases de activo en uso (F9)
SELECT DISTINCT asset_class FROM holdings WHERE user_id = :userId ORDER BY asset_class;
```

```sql
-- Alta implícita de plataforma dentro de la transacción de creación del holding (CA-02.2).
INSERT INTO platforms (user_id, name, type)
VALUES (:userId, :name, 'Other')
ON CONFLICT (user_id, name) DO NOTHING;
```

> Ojo: el `ON CONFLICT` se apoya en la PK `(user_id, name)`. La colisión sólo por diferencia de
> mayúsculas la ataja el índice único `platforms_user_lower_name_uk`, que **no** es un
> `conflict_target` válido en esa forma. El adaptador resuelve primero el nombre canónico con
> `SELECT name FROM platforms WHERE user_id = :userId AND lower(name) = lower(:name)` y usa ese
> nombre; si no existe, inserta. Ambas sentencias van en la misma transacción.

---

## 4. RLS (defensa en profundidad, D5)

El backend conecta con un rol privilegiado y por eso RLS no lo restringe. Se activa igual para que
las tablas nuevas queden cerradas ante PostgREST y el anon key, coherente con `profiles` y `tasks`.

```sql
-- supabase/migrations/V5__wealth_rls.sql
ALTER TABLE platforms           ENABLE ROW LEVEL SECURITY;
ALTER TABLE holdings            ENABLE ROW LEVEL SECURITY;
ALTER TABLE net_worth_snapshots ENABLE ROW LEVEL SECURITY;

CREATE POLICY "own platforms" ON platforms
    FOR ALL USING (auth.uid() = user_id) WITH CHECK (auth.uid() = user_id);
CREATE POLICY "own holdings" ON holdings
    FOR ALL USING (auth.uid() = user_id) WITH CHECK (auth.uid() = user_id);
CREATE POLICY "own snapshots" ON net_worth_snapshots
    FOR ALL USING (auth.uid() = user_id) WITH CHECK (auth.uid() = user_id);
```

**El control primario sigue siendo el `WHERE user_id = ?` de la aplicación** (`plan.md` §5.3), con
sus tests de aislamiento. RLS es la segunda cerradura, no la primera.

---

## 5. Mapeo dominio ⇄ DB ⇄ JSON

| Dominio | Columna | JSON | Notas |
|---|---|---|---|
| `HoldingId(UUID)` | `id UUID` | `"id": "3f2b…"` | El frontend acepta `string` (`Holding.id: number \| string`) — no requiere cambios de tipo |
| `UserId` | `user_id UUID` | **ausente** | Nunca sale en una respuesta: es implícito en el token (CA-01.5) |
| `Money` | `NUMERIC(20,2)` | `12345.67` (número) | `BigDecimal` con `writeBigDecimalAsPlain`; nunca notación científica |
| `AssetClass` | `asset_class TEXT` | `"Crypto"` | El frontend lo llama `cls` → el DTO expone **`assetClass`**; ver §6 |
| `PlatformName` | `platform_name TEXT` | `"Binance"` | El DTO expone `platform`, como hoy |
| `Instant` | `TIMESTAMPTZ` | `"2026-08-23T14:05:00Z"` | `write-dates-as-timestamps: false` ya está configurado |
| `YearMonth` | — | `"2029-08"` | Sólo en la proyección; el nombre del mes lo pone la vista |

### 5.1 Cambios de nombre respecto de los tipos actuales del frontend

Los nombres actuales son abreviaturas de la maqueta HTML original (`cls`, `m`, `v`). La API usa
nombres explícitos; el frontend actualiza `types/wealth.ts` en la fase 6:

| Hoy (frontend) | API | Motivo |
|---|---|---|
| `cls` | `assetClass` | `cls` no significa nada fuera de esa maqueta |
| `value` | `valueUsd` | La moneda deja de ser implícita (riesgo R3) |
| `HistorySnapshot.m` | `capturedAt` (ISO) | Era una etiqueta de presentación, no un dato |
| `HistorySnapshot.v` | `totalValueUsd` | Ídem |

---

## 6. Datos semilla

**Los usuarios nuevos arrancan vacíos** (CA-04.4). Los datos de `INITIAL_HOLDINGS` /
`KvHoldingRepository.seed()` son una demo, no el patrimonio de nadie.

- **Producción:** ninguna semilla. El primer login muestra un estado vacío (que la UI debe soportar
  correctamente — CA-05.4 existe justamente para eso).
- **Local/dev:** un script opcional `backend/scripts/seed-dev.sql`, fuera del código de producción y
  fuera de las migraciones de Flyway, que carga el portafolio de demo para un `user_id` dado.
- **Tests:** fixtures construidas con los factories del dominio (`Holding.create(...)`), nunca SQL
  literal, para que un cambio de invariante rompa las fixtures en compilación.
