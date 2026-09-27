# Guía de Integración Frontend y Contexto para Agentes de IA

> **Documento de especificación y prompt maestro para el Agente / Desarrollador Frontend**  
> Este documento contiene todo el contexto funcional, contratos técnicos, reglas de negocio, esquemas TypeScript y mejores prácticas para conectar o adaptar la aplicación frontend (Next.js en `base_project_fe`) con la API backend de **BASE Wealth Management en Go**.

---

## 1. Misión del Agente Frontend

Tu objetivo es implementar o adaptar la interfaz de usuario en Next.js (App Router, Tailwind CSS, shadcn/ui, TanStack Query / SWR y Supabase Auth) consumiendo exclusivamente los endpoints REST provistos por el backend en Go.

### Principios Fundamentales
1. **Multi-tenancy Estricto y Sin Fuga de Identidad**: El frontend **NUNCA** envía `userId` en el body, query o path. La identidad del usuario se deduce 100% en el backend a través del token JWT de Supabase Auth enviado en el header `Authorization: Bearer <token>`.
2. **Manejo Estándar de Errores RFC 9457**: Todas las respuestas de error siguen `application/problem+json`. Cuando recibas un `400 Bad Request`, debes parsear el arreglo `errors: [{ field, message }]` y mapearlo automáticamente a los campos correspondientes del formulario.
3. **Plataformas Implícitas vs Explícitas**: Al crear un holding con una plataforma inexistente, el backend la crea automáticamente. Al eliminar una plataforma, si tiene holdings asociados el backend responderá `409 Conflict`.
4. **Precisión Numérica y Moneda**: El backend utiliza números decimales exactos (`NUMERIC(20,2)`). El frontend debe formatear los valores monetarios a 2 decimales en USD y sin decimales (o enteros) en ARS.

---

## 2. Configuración y Autenticación con Supabase

### Variables de Entorno del Frontend (`.env.local`)
```env
NEXT_PUBLIC_API_URL=http://localhost:8080
NEXT_PUBLIC_SUPABASE_URL=https://<tu-proyecto>.supabase.co
NEXT_PUBLIC_SUPABASE_ANON_KEY=<tu-anon-key>
```

### Inyección del Token JWT en las Peticiones
Toda solicitud hacia `/api/v1/**` (excepto `/api/v1/health`) debe adjuntar el token de sesión de Supabase:

```typescript
// lib/apiClient.ts
import { createClient } from '@supabase/supabase-js';

const supabase = createClient(
  process.env.NEXT_PUBLIC_SUPABASE_URL!,
  process.env.NEXT_PUBLIC_SUPABASE_ANON_KEY!
);

export async function fetchWithAuth(endpoint: string, options: RequestInit = {}) {
  const { data: { session } } = await supabase.auth.getSession();
  const token = session?.access_token;

  if (!token && endpoint !== '/api/v1/health') {
    throw new Error('No hay sesión activa de Supabase');
  }

  const headers = new Headers(options.headers);
  if (token) {
    headers.set('Authorization', `Bearer ${token}`);
  }
  if (!headers.has('Content-Type') && options.body && !(options.body instanceof FormData)) {
    headers.set('Content-Type', 'application/json');
  }

  const response = await fetch(`${process.env.NEXT_PUBLIC_API_URL}${endpoint}`, {
    ...options,
    headers,
  });

  if (!response.ok) {
    let errorProblem: ProblemDetail | null = null;
    try {
      errorProblem = await response.json();
    } catch {
      // No era JSON
    }
    throw new ApiError(response.status, errorProblem || {
      type: 'about:blank',
      title: response.statusText,
      status: response.status,
      detail: 'Error inesperado del servidor',
    });
  }

  if (response.status === 204) {
    return null;
  }

  return response.json();
}
```

---

## 3. Catálogo Completo de Endpoints y Reglas de Negocio

### 3.1. Monitoreo (Público)

#### `GET /api/v1/health`
- **Descripción**: Permite comprobar que el backend está vivo y accesible.
- **Seguridad**: Público (sin Bearer token).
- **Respuesta 200**:
  ```json
  {
    "status": "UP",
    "service": "base-wealth-backend",
    "timestamp": "2026-09-27T18:00:00Z",
    "environment": "production-ready",
    "version": "1.0.0"
  }
  ```

---

### 3.2. Módulo de Patrimonio (`Wealth`)

#### `GET /api/v1/wealth/summary`
- **Descripción**: Vista consolidada principal del Dashboard.
- **Datos Devueltos**:
  - `netWorth`:
    - `usd`: Total en dólares estadounidenses.
    - `ars`: Total en pesos argentinos (`usd * fxRate.value`). Es `null` si `fxRate.available` es `false`.
    - `fxRate`: Objeto con `available: boolean`, `value: number`, `asOf: ISO8601`, `source: string`.
  - `holdingsCount`: Total de activos/posiciones registradas.
  - `ytd`: Rendimiento Year-To-Date.
    - `basis`: Puede ser `"YEAR_START_SNAPSHOT"` (comparado con el 1° de enero), `"EARLIEST_SNAPSHOT"` (primer snapshot histórico si el usuario empezó a mitad de año), o `"NO_BASELINE"` (si no hay historial aún).
    - `growthPct`: Porcentaje con signo y 1 decimal (ej. `12.5` o `-3.2`).
    - `baselineValueUsd` y `baselineAt`: Valores del snapshot tomado como base.
  - `liquidity`: Desglose de liquidez.
    - `liquidPct`: Porcentaje de activos líquidos (Cash, Equity, Crypto, Index Fund).
    - `illiquidPct`: Porcentaje de activos ilíquidos (Fixed Income, Real Estate, etc.).
    - `liquidAssetClasses`: Lista de clases consideradas líquidas.
  - `byAssetClass`: Lista de `{ assetClass, valueUsd, pct, count }`.
  - `byPlatform`: Lista de `{ name, type, valueUsd, pct, count }`. **Nota:** Incluye plataformas en saldo $0 para mostrar cuentas vacías.
- **Ejemplo de Respuesta 200**:
  ```json
  {
    "netWorth": {
      "usd": 84250.00,
      "ars": 88462500.00,
      "fxRate": {
        "available": true,
        "value": 1050.00,
        "asOf": "2026-09-27T18:00:00Z",
        "source": "FIXED_CONFIG"
      }
    },
    "holdingsCount": 8,
    "ytd": {
      "basis": "YEAR_START_SNAPSHOT",
      "growthPct": 12.5,
      "baselineValueUsd": 74888.89,
      "baselineAt": "2026-01-01T00:00:00Z"
    },
    "liquidity": {
      "liquidPct": 75.2,
      "illiquidPct": 24.8,
      "liquidAssetClasses": ["Cash", "Equity", "Crypto", "Index Fund"]
    },
    "byAssetClass": [
      { "assetClass": "Equity", "valueUsd": 42125.00, "pct": 50.0, "count": 3 },
      { "assetClass": "Crypto", "valueUsd": 15000.00, "pct": 17.8, "count": 2 }
    ],
    "byPlatform": [
      { "name": "Balanz", "type": "Broker", "valueUsd": 63025.00, "pct": 74.8, "count": 5 },
      { "name": "Binance", "type": "Exchange", "valueUsd": 15000.00, "pct": 17.8, "count": 2 },
      { "name": "Interactive Brokers", "type": "Broker", "valueUsd": 0.00, "pct": 0.0, "count": 0 }
    ]
  }
  ```

#### `GET /api/v1/wealth/estimate`
- **Descripción**: Simulador / Calculadora de interés compuesto e hitos financieros.
- **Query Parameters**:
  - `contribution` *(requerido, number)*: Aporte mensual en USD (0 a 1,000,000,000).
  - `yieldPct` *(requerido, number)*: Tasa de rendimiento anual estimada en % (0 a 100).
  - `years` *(requerido, integer)*: Plazo en años (1 a 50).
  - `milestones` *(opcional, lista)*: Montos de metas en USD separados por coma (ej. `150000,250000`). Máximo 5 metas.
  - `principal` *(opcional, number)*: Capital inicial. Si se omite, el backend usa el patrimonio neto actual del usuario.
- **Cache**: La respuesta incluye `Cache-Control: private, max-age=30`.
- **Estructura de Respuesta**:
  - `series`: Arreglo de $N+1$ puntos (año 0 hasta año $N$). Cada punto contiene `{ year, futureValueUsd, totalContributedUsd, interestEarnedUsd }`.
  - `milestones`: Arreglo con la evaluación de cada meta:
    - `status`: `"ACHIEVED"` (ya superada hoy), `"REACHABLE"` (alcanzable en el horizonte), o `"OUT_OF_HORIZON"` (no se alcanza dentro de los años simulados).
    - `monthsRequired`: Meses requeridos (0 si es `ACHIEVED`, `null` si es `OUT_OF_HORIZON`).
    - `targetMonth`: Mes calendario en formato `"YYYY-MM"` (ej. `"2029-11"`). `null` si no es alcanzable o si ya fue lograda.

#### `GET /api/v1/wealth/snapshots`
- **Descripción**: Histórico de evolución patrimonial para graficar la curva real de patrimonio en el tiempo.
- **Query Parameters**:
  - `from` *(opcional)*: Fecha inicio en formato `YYYY-MM-DD`.
  - `to` *(opcional)*: Fecha fin en formato `YYYY-MM-DD`.
- **Respuesta 200**:
  ```json
  [
    {
      "id": "c138b812-70b9-43a9-a9a3-5c0a3733e8b1",
      "capturedAt": "2026-08-01T12:00:00Z",
      "totalValueUsd": 78500.00,
      "changePctFromPrevious": null
    },
    {
      "id": "741e176b-9c29-478e-a2b1-6a2d9a6c99c2",
      "capturedAt": "2026-09-01T12:00:00Z",
      "totalValueUsd": 84250.00,
      "changePctFromPrevious": 7.3
    }
  ]
  ```

#### `POST /api/v1/wealth/snapshots`
- **Descripción**: Toma una "foto" instantánea del patrimonio actual del usuario y la guarda en la base de datos.
- **Body**: Vacío (`{}` o sin body).
- **Respuesta**: `201 Created` con header `Location: /api/v1/wealth/snapshots/{id}`.
- **Error 409 Conflict**: Si el usuario hace doble clic o envía más de una solicitud en el mismo segundo calendario. El frontend debe deshabilitar el botón durante el envío y mostrar un mensaje amigable si ocurre un `409`.

---

### 3.3. Módulo de Posiciones (`Holdings`)

#### `GET /api/v1/holdings`
- **Query Params**:
  - `assetClass` *(opcional, string)*: Filtra por categoría exacta.
  - `platform` *(opcional, string)*: Filtra por nombre de plataforma.
- **Respuesta 200**: Lista de holdings ordenados por fecha de creación ascendente.

#### `POST /api/v1/holdings`
- **Body**:
  ```json
  {
    "name": "Apple Inc.",
    "assetClass": "Equity",
    "platform": "Balanz",
    "valueUsd": 12500.00
  }
  ```
- **Regla Fundamental**: Si `"Balanz"` no existe previamente como plataforma para este usuario, el backend **la crea automáticamente**. El frontend no necesita obligar al usuario a dar de alta la plataforma primero.
- **Respuesta 201**: Retorna el holding creado y el header `Location: /api/v1/holdings/{id}`.

#### `GET /api/v1/holdings/{id}`
- **Respuesta 200**: Detalle del holding. `404 Not Found` si no existe.

#### `PATCH /api/v1/holdings/{id}`
- **Body** (todos los campos son opcionales):
  ```json
  {
    "name": "Apple Inc. (AAPL)",
    "valueUsd": 13200.00
  }
  ```
- **Respuesta 200**: Holding con datos actualizados y `updatedAt` refrescado.

#### `DELETE /api/v1/holdings/{id}`
- **Respuesta 204**: No Content al eliminar exitosamente.

---

### 3.4. Módulo de Plataformas (`Platforms`)

#### `GET /api/v1/platforms`
- **Respuesta 200**: Lista de plataformas registradas (`name`, `type`, `createdAt`).

#### `POST /api/v1/platforms`
- **Body**:
  ```json
  {
    "name": "Binance",
    "type": "Exchange"
  }
  ```
- **Regla de Unicidad**: Insensible a mayúsculas y minúsculas (`lower(name)`). Si se intenta crear `"binance"` existiendo ya `"Binance"`, el backend responde `409 Conflict`.

#### `PATCH /api/v1/platforms/{name}`
- **Body**:
  ```json
  {
    "name": "Binance Global",
    "type": "Crypto Exchange"
  }
  ```
- **Comportamiento en BD**: Si se renombra la plataforma, la base de datos actualiza en cascada (`ON UPDATE CASCADE`) el campo `platform_name` en todas las posiciones (`holdings`) asociadas.

#### `DELETE /api/v1/platforms/{name}`
- **Regla de Integridad Referencial**:
  - Si la plataforma **tiene holdings asociados**: El backend rechaza la eliminación con `409 Conflict` (`Platform '...' still has N holdings`).
  - **Acción UI recomendada**: Mostrar modal al usuario indicando que debe reasignar o eliminar los activos de esa plataforma antes de borrarla.
  - Si no tiene holdings: Responde `204 No Content`.

---

### 3.5. Módulo de Clases de Activo (`Asset Classes`)

#### `GET /api/v1/asset-classes`
- **Respuesta 200**:
  ```json
  {
    "defaults": ["Cash", "Fixed Income", "Index Fund", "Equity", "Crypto"],
    "inUse": ["Cash", "Equity", "Real Estate"],
    "all": ["Cash", "Fixed Income", "Index Fund", "Equity", "Crypto", "Real Estate"]
  }
  ```
- **Uso en UI**: Alimenta el `<Select>` o componente de autocompletado en el formulario de creación/edición de holding. Se recomienda permitir al usuario escribir una clase personalizada (Free Text, hasta 60 caracteres).

---

## 4. Definiciones de Tipos TypeScript (Copiar y Pegar en el Frontend)

```typescript
// types/api.ts

export type YtdBasis = 'YEAR_START_SNAPSHOT' | 'EARLIEST_SNAPSHOT' | 'NO_BASELINE';

export type MilestoneStatus = 'ACHIEVED' | 'REACHABLE' | 'OUT_OF_HORIZON';

export interface FxRateDTO {
  available: boolean;
  value?: number;
  asOf?: string;
  source?: string;
}

export interface NetWorthDTO {
  usd: number;
  ars?: number | null;
  fxRate: FxRateDTO;
}

export interface YtdDTO {
  basis: YtdBasis;
  growthPct: number;
  baselineValueUsd?: number | null;
  baselineAt?: string | null;
}

export interface LiquidityDTO {
  liquidPct: number;
  illiquidPct: number;
  liquidAssetClasses: string[];
}

export interface AssetClassBreakdown {
  assetClass: string;
  valueUsd: number;
  pct: number;
  count: number;
}

export interface PlatformBreakdown {
  name: string;
  type: string;
  valueUsd: number;
  pct: number;
  count: number;
}

export interface WealthSummaryResponse {
  netWorth: NetWorthDTO;
  holdingsCount: number;
  ytd: YtdDTO;
  liquidity: LiquidityDTO;
  byAssetClass: AssetClassBreakdown[];
  byPlatform: PlatformBreakdown[];
}

export interface ProjectionPoint {
  year: number;
  futureValueUsd: number;
  totalContributedUsd: number;
  interestEarnedUsd: number;
}

export interface Milestone {
  amountUsd: number;
  status: MilestoneStatus;
  monthsRequired?: number | null;
  targetMonth?: string | null; // "YYYY-MM"
}

export interface ProjectionResponse {
  principalUsd: number;
  monthlyContributionUsd: number;
  annualYieldPct: number;
  years: number;
  series: ProjectionPoint[];
  milestones: Milestone[];
}

export interface SnapshotResponse {
  id: string;
  capturedAt: string;
  totalValueUsd: number;
  changePctFromPrevious?: number | null;
}

export interface HoldingResponse {
  id: string;
  name: string;
  assetClass: string;
  platform: string;
  valueUsd: number;
  createdAt: string;
  updatedAt: string;
}

export interface CreateHoldingRequest {
  name: string;
  assetClass: string;
  platform: string;
  valueUsd: number;
}

export interface UpdateHoldingRequest {
  name?: string;
  assetClass?: string;
  platform?: string;
  valueUsd?: number;
}

export interface PlatformResponse {
  name: string;
  type: string;
  createdAt: string;
}

export interface CreatePlatformRequest {
  name: string;
  type?: string;
}

export interface PatchPlatformRequest {
  name?: string;
  type?: string;
}

export interface AvailableAssetClassesResponse {
  defaults: string[];
  inUse: string[];
  all: string[];
}

// RFC 9457 Problem Details
export interface FieldError {
  field: string;
  message: string;
}

export interface ProblemDetail {
  type: string;
  title: string;
  status: number;
  detail: string;
  instance?: string;
  traceId?: string;
  errors?: FieldError[];
}

export class ApiError extends Error {
  constructor(public status: number, public problem: ProblemDetail) {
    super(problem.detail || problem.title || `HTTP error ${status}`);
    this.name = 'ApiError';
  }
}
```

---

## 5. Implementación de Servicios y Hooks en React (TanStack Query)

```typescript
// hooks/useWealth.ts
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { fetchWithAuth } from '@/lib/apiClient';
import type { 
  WealthSummaryResponse, 
  ProjectionResponse, 
  SnapshotResponse, 
  HoldingResponse,
  CreateHoldingRequest 
} from '@/types/api';

// 1. Resumen Patrimonial
export function useWealthSummary() {
  return useQuery<WealthSummaryResponse>({
    queryKey: ['wealth', 'summary'],
    queryFn: () => fetchWithAuth('/api/v1/wealth/summary'),
    staleTime: 1000 * 60, // 1 minuto
  });
}

// 2. Simulador de Proyecciones
export function useWealthEstimate(params: {
  contribution: number;
  yieldPct: number;
  years: number;
  milestones?: number[];
  principal?: number;
}) {
  const query = new URLSearchParams({
    contribution: params.contribution.toString(),
    yieldPct: params.yieldPct.toString(),
    years: params.years.toString(),
  });
  if (params.milestones && params.milestones.length > 0) {
    query.set('milestones', params.milestones.join(','));
  }
  if (params.principal !== undefined) {
    query.set('principal', params.principal.toString());
  }

  return useQuery<ProjectionResponse>({
    queryKey: ['wealth', 'estimate', params],
    queryFn: () => fetchWithAuth(`/api/v1/wealth/estimate?${query.toString()}`),
    staleTime: 1000 * 30, // Respeta Cache-Control de 30s
  });
}

// 3. Captura de Snapshot
export function useCreateSnapshot() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => fetchWithAuth('/api/v1/wealth/snapshots', { method: 'POST' }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['wealth', 'snapshots'] });
      queryClient.invalidateQueries({ queryKey: ['wealth', 'summary'] });
    },
  });
}

// 4. Crear Holding con invalidación reactiva
export function useCreateHolding() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (data: CreateHoldingRequest) => 
      fetchWithAuth('/api/v1/holdings', {
        method: 'POST',
        body: JSON.stringify(data),
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['holdings'] });
      queryClient.invalidateQueries({ queryKey: ['platforms'] });
      queryClient.invalidateQueries({ queryKey: ['asset-classes'] });
      queryClient.invalidateQueries({ queryKey: ['wealth', 'summary'] });
    },
  });
}
```

---

## 6. Mapeo de Errores RFC 9457 en Formularios de React Hook Form

```typescript
// utils/handleFormError.ts
import { UseFormSetError, FieldValues, Path } from 'react-hook-form';
import { ApiError } from '@/types/api';

export function applyApiValidationErrors<T extends FieldValues>(
  error: unknown,
  setError: UseFormSetError<T>,
  fallbackMessage: (msg: string) => void
) {
  if (error instanceof ApiError && error.problem.errors) {
    error.problem.errors.forEach((err) => {
      setError(err.field as Path<T>, {
        type: 'server',
        message: err.message,
      });
    });
  } else if (error instanceof ApiError) {
    fallbackMessage(error.problem.detail);
  } else if (error instanceof Error) {
    fallbackMessage(error.message);
  }
}
```

---

## 7. Checklist para el Agente Frontend
- [ ] Configurar variables de entorno con `NEXT_PUBLIC_API_URL`.
- [ ] Configurar cliente HTTP para enviar `Authorization: Bearer <token>`.
- [ ] Comprobar que en ninguna petición se envíe `userId`.
- [ ] Manejar respuestas `409` en captura de snapshots y eliminación de plataformas.
- [ ] Renderizar los gráficos de distribución por clase de activo y plataforma usando los datos de `/api/v1/wealth/summary`.
- [ ] Sincronizar el simulador de interés compuesto con `/api/v1/wealth/estimate` y pintar los hitos (`ACHIEVED`, `REACHABLE` con fecha, `OUT_OF_HORIZON`).
- [ ] Utilizar `/api/v1/asset-classes` para alimentar opciones en los selectores de activos.
