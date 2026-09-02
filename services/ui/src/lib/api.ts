const API_BASE = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8000';

export async function submitApplication(data: any, idempotencyKey?: string) {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  };
  if (idempotencyKey) {
    headers['Idempotency-Key'] = idempotencyKey;
  }
  const res = await fetch(`${API_BASE}/api/v1/applications`, {
    method: 'POST',
    headers,
    body: JSON.stringify(data),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'Submission failed' }));
    throw new Error(err.error || 'Failed to submit application');
  }
  return res.json();
}

export async function fetchApplication(id: string) {
  const res = await fetch(`${API_BASE}/api/v1/applications/get?id=${id}`);
  if (!res.ok) throw new Error('Application not found');
  return res.json();
}

export async function listApplications() {
  const res = await fetch(`${API_BASE}/api/v1/applications`);
  if (!res.ok) throw new Error('Failed to fetch applications');
  return res.json();
}

export async function reviewDecision(appId: string, decision: 'APPROVE' | 'DECLINE', limitCents: number, aprBps: number, notes: string) {
  const res = await fetch(`${API_BASE}/api/v1/applications/review?id=${appId}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      decision,
      approved_limit_cents: limitCents,
      approved_apr_bps: aprBps,
      notes,
    }),
  });
  if (!res.ok) throw new Error('Failed to submit review');
  return res.json();
}

export async function fetchAuditLedger() {
  const res = await fetch(`${API_BASE}/api/v1/governance/ledger`);
  if (!res.ok) throw new Error('Failed to fetch audit ledger');
  return res.json();
}
