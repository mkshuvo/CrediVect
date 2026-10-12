// Typed client for the CrediVect core API.
//
// Requests are sent to this UI's own origin and proxied to core by the Next.js
// rewrite in next.config.ts, so core's host port never matters to the browser.
// The session token issued by /auth/login is attached to every authenticated
// call; core rejects the queue, ledger, governance and review endpoints without
// it, so there is no demo-mode fallback here.

const API_BASE = process.env.NEXT_PUBLIC_API_URL || '';

const TOKEN_KEY = 'credivect_token';
const USER_KEY = 'credivect_user';

export type Role = 'BORROWER' | 'UNDERWRITER' | 'ADMIN';

export interface SessionUser {
  id: string;
  email: string;
  name: string;
  role: Role;
}

export interface HealthResponse {
  service: string;
  checks: Record<string, string>;
  model_version: string;
  event_stream: boolean;
  bureau_provider: string;
}

export interface Attribution {
  feature_name: string;
  feature_value: string;
  shap_value: number;
  rank: number;
}

export interface AdverseReason {
  reason_code: string;
  description: string;
  priority_rank: number;
}

export interface ScoringResult {
  id: string;
  application_id?: string;
  model_version_id?: string;
  scorecard_points: number;
  calibrated_pd: string;
  pd_basis_points: number;
  raw_ml_logit: number;
  risk_tier: string;
  recommended_outcome: string;
  max_approved_limit_cents: number;
  recommended_apr_bps: number;
  execution_time_micros: number;
  scored_at: string;
  model_version: string;
  feature_attributions: Attribution[];
  adverse_action_reasons: AdverseReason[];
}

export interface BureauReport {
  id: string;
  application_id: string;
  bureau_name: string;
  bureau_score: number;
  total_debt_cents: number;
  revolving_balance_cents: number;
  revolving_limit_cents: number;
  revolving_utilization_bps: number;
  delinquencies_24m: number;
  inquiries_6m: number;
  total_trades: number;
  open_trades: number;
  bankruptcy_flag: boolean;
  age_of_oldest_trade_months: number;
  valid_time: string;
}

export interface Decision {
  id: string;
  outcome: string;
  approved_limit_cents: number;
  approved_apr_bps: number;
  decision_type: string;
  underwriter_id?: string;
  decision_notes?: string;
  decisioned_at: string;
}

export interface Application {
  id: string;
  applicant_id: string;
  applicant?: Applicant;
  requested_amount_cents: number;
  currency_code: string;
  loan_term_months: number;
  loan_purpose: string;
  status: 'SUBMITTED' | 'IN_REVIEW' | 'APPROVED' | 'DECLINED' | 'CANCELLED';
  workflow_id: string;
  version: number;
  created_at: string;
  updated_at: string;
  bureau_report?: BureauReport;
  scoring_result?: ScoringResult;
  decision?: Decision;
}

export interface Applicant {
  id: string;
  external_id: string;
  first_name: string;
  last_name: string;
  email: string;
  date_of_birth: string;
  monthly_income_cents: number;
  currency_code: string;
  employment_status: string;
  created_at: string;
}

export interface ApplicationPage {
  applications: Application[];
  count: number;
  limit: number;
  offset: number;
  by_status: Record<string, number>;
}

export interface WorkflowState {
  stage: string;
  risk_tier?: string;
  scorecard_points?: number;
  outcome?: string;
  waiting_since?: string;
}

export interface LatencyReport {
  model_version: string;
  sla_target_ms: number;
  measured: {
    samples: number;
    p50_micros: number;
    p95_micros: number;
    p99_micros: number;
    max_micros: number;
    mean_micros: number;
    share_within_15ms: number;
    window_started_at: string;
  };
  sla_met: boolean;
}

export interface ChainVerification {
  verified: boolean;
  entries_checked: number;
  head_hash: string;
  broken_at_entry_id?: string;
  reason?: string;
}

export interface ModelVersionInfo {
  id: string;
  model_name: string;
  version: string;
  algorithm: string;
  dataset_sha256: string;
  is_active: boolean;
  deployed_at: string;
}

export interface DriftMetric {
  id: string;
  model_version: string;
  feature_name: string;
  psi_value: number;
  sample_size: number;
  status: string;
  window_start: string;
  window_end: string;
  evaluated_at: string;
}

export interface DriftBin {
  lower: number;
  upper: number;
  expected_share: number;
  actual_share: number;
  count: number;
  psi_contribution: number;
}

export interface AuditEntry {
  id: string;
  application_id: string;
  event_type: string;
  actor: string;
  payload_sha256: string;
  previous_entry_hash: string;
  entry_hash: string;
  metadata: string;
  recorded_at: string;
}

export interface AdverseNotice {
  application_id: string;
  decided_at: string;
  outcome: string;
  creditor: { name: string; address: string; phone: string };
  reporting_agency: { name: string; address: string; phone: string };
  score_used: {
    bureau_score: number;
    scorecard_points: number;
    calibrated_pd: string;
    risk_tier: string;
    score_source: string;
  };
  reasons: AdverseReason[];
  key_factors: Attribution[];
  model_version: string;
  consumer_rights: {
    free_report_window_days: number;
    can_dispute_accuracy: boolean;
    can_request_reasons: boolean;
  };
}

export class ApiError extends Error {
  status: number;
  fieldErrors: Record<string, string>;

  constructor(message: string, status: number, fieldErrors: Record<string, string> = {}) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.fieldErrors = fieldErrors;
  }
}

export function getToken(): string | null {
  if (typeof window === 'undefined') return null;
  return window.localStorage.getItem(TOKEN_KEY);
}

export function getStoredUser(): SessionUser | null {
  if (typeof window === 'undefined') return null;
  const raw = window.localStorage.getItem(USER_KEY);
  if (!raw) return null;
  try {
    return JSON.parse(raw) as SessionUser;
  } catch {
    return null;
  }
}

export function saveSession(token: string, user: SessionUser) {
  window.localStorage.setItem(TOKEN_KEY, token);
  window.localStorage.setItem(USER_KEY, JSON.stringify(user));
}

export function clearSession() {
  window.localStorage.removeItem(TOKEN_KEY);
  window.localStorage.removeItem(USER_KEY);
}

interface RequestOptions {
  method?: 'GET' | 'POST';
  body?: unknown;
  headers?: Record<string, string>;
  anonymous?: boolean;
}

async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json', ...(opts.headers || {}) };
  const token = getToken();
  if (token && !opts.anonymous) headers.Authorization = `Bearer ${token}`;

  let res: Response;
  try {
    res = await fetch(`${API_BASE}/api/v1${path}`, {
      method: opts.method || 'GET',
      headers,
      body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
    });
  } catch {
    throw new ApiError('Cannot reach the core service. Is the Docker stack running?', 0);
  }

  const text = await res.text();
  let payload: any = null;
  if (text) {
    try {
      payload = JSON.parse(text);
    } catch {
      payload = null;
    }
  }
  if (!res.ok) {
    const message =
      (payload && (payload.error || payload.message)) ||
      `Request failed with status ${res.status}`;
    throw new ApiError(message, res.status, (payload && payload.field_errors) || {});
  }
  return (payload ?? {}) as T;
}

export interface ReviewRequest {
  decision: 'APPROVE' | 'DECLINE';
  approved_limit_cents: number;
  approved_apr_bps: number;
  notes: string;
}

export interface NewApplication {
  first_name: string;
  last_name: string;
  email: string;
  date_of_birth: string;
  ssn: string;
  monthly_income_cents: number;
  requested_amount_cents: number;
  loan_term_months: number;
  loan_purpose: string;
  employment_status: string;
  consent_fcra: boolean;
}

export const api = {
  health: () => request<HealthResponse>('/health', { anonymous: true }),

  login: (email: string, password: string) =>
    request<{ token: string; expires_at: string; user: SessionUser }>('/auth/login', {
      method: 'POST',
      body: { email, password },
      anonymous: true,
    }),

  me: () => request<SessionUser>('/auth/me'),

  submitApplication: (payload: NewApplication, idempotencyKey: string) =>
    request<{ application: Application; workflow_id: string; status: string }>('/applications', {
      method: 'POST',
      body: payload,
      headers: { 'Idempotency-Key': idempotencyKey },
    }),

  listApplications: (params: { status?: string; tier?: string; limit?: number; offset?: number } = {}) => {
    const q = new URLSearchParams();
    if (params.status) q.set('status', params.status);
    if (params.tier) q.set('tier', params.tier);
    q.set('limit', String(params.limit ?? 25));
    q.set('offset', String(params.offset ?? 0));
    return request<ApplicationPage>(`/applications?${q.toString()}`);
  },

  getApplication: (id: string) => request<Application>(`/applications/get?id=${encodeURIComponent(id)}`),

  workflowState: (id: string) =>
    request<WorkflowState>(`/applications/workflow-state?id=${encodeURIComponent(id)}`),

  review: (id: string, body: ReviewRequest) =>
    request<{ application_id: string; workflow_id: string; signal: string; decision: string }>(
      `/applications/review?id=${encodeURIComponent(id)}`,
      { method: 'POST', body },
    ),

  adverseAction: (id: string) => request<AdverseNotice>(`/applications/adverse-action?id=${encodeURIComponent(id)}`),

  // The notice is a server-rendered text attachment; a plain link downloads it
  // with the statutory wording already formatted.
  adverseActionNoticeUrl: (id: string) => `/api/v1/applications/adverse-action.txt?id=${encodeURIComponent(id)}`,

  ledger: (limit = 50, applicationId?: string) =>
    request<AuditEntry[]>(`/governance/ledger?limit=${limit}${applicationId ? `&application_id=${encodeURIComponent(applicationId)}` : ''}`),

  verifyChain: () => request<ChainVerification>('/governance/verify'),

  latency: (samples = 2000) => request<LatencyReport>(`/governance/latency?samples=${samples}`),

  modelVersion: () =>
    request<{ model_version: ModelVersionInfo; served_by_engine: string; consistent: boolean }>('/governance/model'),

  drift: (limit = 20) => request<DriftMetric[]>(`/governance/drift?limit=${limit}`),

  // Re-measures the live scored population against the shipped training
  // baseline and persists the reading; gated to ADMIN by core.
  evaluateDrift: () =>
    request<{ metric: DriftMetric; bins: DriftBin[] }>('/governance/drift', { method: 'POST', body: {} }),
};
