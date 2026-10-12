"use client";

import React, { useCallback, useEffect, useState } from 'react';
import Link from 'next/link';
import {
  Alert,
  Box,
  Button,
  Card,
  CardContent,
  Chip,
  CircularProgress,
  Divider,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  Typography,
} from '@mui/material';
import VerifiedUserIcon from '@mui/icons-material/VerifiedUser';
import SpeedIcon from '@mui/icons-material/Speed';
import ModelTrainingIcon from '@mui/icons-material/ModelTraining';
import AnalyticsIcon from '@mui/icons-material/Analytics';
import { api, ApiError } from '@/lib/api';
import type { AuditEntry, ChainVerification, DriftMetric, LatencyReport, ModelVersionInfo } from '@/lib/api';
import { useAuth } from '@/lib/auth/AuthContext';

const LEDGER_ROWS = 60;

export default function GovernancePage() {
  const { role, isAuthenticated, ready } = useAuth();
  const [latency, setLatency] = useState<LatencyReport | null>(null);
  const [chain, setChain] = useState<ChainVerification | null>(null);
  const [model, setModel] = useState<{ model_version: ModelVersionInfo; served_by_engine: string; consistent: boolean } | null>(
    null,
  );
  const [drift, setDrift] = useState<DriftMetric[]>([]);
  const [ledger, setLedger] = useState<AuditEntry[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [verifying, setVerifying] = useState(false);
  const [measuring, setMeasuring] = useState(false);

  const mayViewGovernance = role === 'UNDERWRITER' || role === 'ADMIN';
  const mayMeasureDrift = role === 'ADMIN';

  const load = useCallback(async () => {
    if (!isAuthenticated || !mayViewGovernance) return;
    // Each card reads a different endpoint and fails on its own terms: a 404 from
    // the model registry must not blank the ledger.
    const results = await Promise.allSettled([
      api.latency(2000),
      api.verifyChain(),
      api.modelVersion(),
      api.drift(20),
      api.ledger(LEDGER_ROWS),
    ]);
    const take = <T,>(index: number): T | null =>
      results[index].status === 'fulfilled' ? (results[index] as PromiseFulfilledResult<T>).value : null;

    setLatency(take<LatencyReport>(0));
    setChain(take<ChainVerification>(1));
    setModel(take<{ model_version: ModelVersionInfo; served_by_engine: string; consistent: boolean }>(2));
    setDrift(take<DriftMetric[]>(3) ?? []);
    setLedger(take<AuditEntry[]>(4) ?? []);

    const failed = results.find((r) => r.status === 'rejected');
    setError(failed && failed.status === 'rejected' ? describe(failed.reason) : null);
    setLoading(false);
  }, [isAuthenticated, mayViewGovernance]);

  useEffect(() => {
    if (ready) load();
  }, [ready, load]);

  const reverify = async () => {
    setVerifying(true);
    try {
      setChain(await api.verifyChain());
      setError(null);
    } catch (err) {
      setError(describe(err));
    } finally {
      setVerifying(false);
    }
  };

  const measureDrift = async () => {
    setMeasuring(true);
    try {
      await api.evaluateDrift();
      setDrift(await api.drift(20));
      setLedger(await api.ledger(LEDGER_ROWS));
      setError(null);
    } catch (err) {
      setError(describe(err));
    } finally {
      setMeasuring(false);
    }
  };

  if (!ready) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', py: 10 }}>
        <CircularProgress color="secondary" />
      </Box>
    );
  }

  if (!isAuthenticated || !mayViewGovernance) {
    return (
      <Box sx={{ py: 8, px: { xs: 2, md: 4 }, maxWidth: 1400, mx: 'auto' }}>
        <Alert
          severity="warning"
          action={
            <Button component={Link} href="/login" color="inherit" size="small">
              Sign In
            </Button>
          }
        >
          Governance and audit data requires an UNDERWRITER or ADMIN session.
        </Alert>
      </Box>
    );
  }

  const sla = latency?.measured;

  return (
    <Box sx={{ py: 4, px: { xs: 2, md: 4 }, maxWidth: 1400, mx: 'auto' }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 3, flexWrap: 'wrap', gap: 2 }}>
        <Typography variant="h5" sx={{ fontWeight: 800 }}>
          Governance &amp; Audit
        </Typography>
        <Button size="small" onClick={load} disabled={loading}>
          Reload
        </Button>
      </Box>

      {error && (
        <Alert severity="warning" sx={{ mb: 3 }} onClose={() => setError(null)}>
          {error}
        </Alert>
      )}

      <Box sx={{ display: 'flex', gap: 3, flexWrap: 'wrap', mb: 4 }}>
        {/* Latency: measured from persisted scoring runs, not asserted */}
        <Card sx={{ flex: '1 1 320px' }}>
          <CardContent>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
              <SpeedIcon color="secondary" />
              <Typography variant="subtitle1" sx={{ fontWeight: 700 }}>
                Scoring Latency vs 15 ms SLA
              </Typography>
            </Box>
            {sla ? (
              <>
                <Typography variant="h4" sx={{ fontWeight: 800, color: latency?.sla_met ? 'success.dark' : 'error.dark' }}>
                  p95 {(sla.p95_micros / 1000).toFixed(2)} ms
                </Typography>
                <Metric label="samples" value={String(sla.samples)} />
                <Metric label="p50" value={`${(sla.p50_micros / 1000).toFixed(2)} ms`} />
                <Metric label="p99" value={`${(sla.p99_micros / 1000).toFixed(2)} ms`} />
                <Metric label="max" value={`${(sla.max_micros / 1000).toFixed(2)} ms`} />
                <Metric label="mean" value={`${(sla.mean_micros / 1000).toFixed(2)} ms`} />
                <Metric label="within 15 ms" value={`${(sla.share_within_15ms * 100).toFixed(1)}%`} />
                <Chip
                  size="small"
                  sx={{ mt: 1 }}
                  color={latency?.sla_met ? 'success' : 'error'}
                  label={latency?.sla_met ? 'SLA MET' : 'SLA BREACHED'}
                />
                <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }}>
                  engine-time percentiles over the {sla.samples} most recent scoring runs, model {latency?.model_version}
                </Typography>
              </>
            ) : (
              <Typography variant="body2" color="text.secondary">
                No scoring runs recorded yet.
              </Typography>
            )}
          </CardContent>
        </Card>

        {/* Chain: recomputed by the server on demand */}
        <Card sx={{ flex: '1 1 320px' }}>
          <CardContent>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
              <VerifiedUserIcon color={chain?.verified ? 'success' : 'error'} />
              <Typography variant="subtitle1" sx={{ fontWeight: 700 }}>
                Merkle Audit Chain
              </Typography>
            </Box>
            {chain ? (
              <>
                <Chip
                  size="small"
                  color={chain.verified ? 'success' : 'error'}
                  label={chain.verified ? 'CHAIN VERIFIED' : 'CHAIN BROKEN'}
                  sx={{ mb: 1 }}
                />
                <Metric label="entries checked" value={String(chain.entries_checked)} />
                <Metric label="head hash" value={short(chain.head_hash)} mono />
                {!chain.verified && <Metric label="broken at" value={short(chain.broken_at_entry_id || '')} mono />}
                {!chain.verified && chain.reason && <Metric label="reason" value={chain.reason} />}
              </>
            ) : (
              <Typography variant="body2" color="text.secondary">
                Verification unavailable.
              </Typography>
            )}
            <Button size="small" variant="outlined" onClick={reverify} disabled={verifying} sx={{ mt: 1.5 }}>
              {verifying ? 'Recomputing...' : 'Re-verify chain'}
            </Button>
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }}>
              SHA256(prev || payload || timestamp) recomputed from the genesis constant
            </Typography>
          </CardContent>
        </Card>

        {/* Model registry vs what the gRPC server actually serves */}
        <Card sx={{ flex: '1 1 320px' }}>
          <CardContent>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
              <ModelTrainingIcon color="primary" />
              <Typography variant="subtitle1" sx={{ fontWeight: 700 }}>
                Active Model
              </Typography>
            </Box>
            {model ? (
              <>
                <Typography variant="h6" sx={{ fontWeight: 800 }}>
                  {model.model_version.model_name} v{model.model_version.version}
                </Typography>
                <Metric label="algorithm" value={model.model_version.algorithm} />
                <Metric label="served by engine" value={model.served_by_engine} />
                <Metric label="dataset sha256" value={short(model.model_version.dataset_sha256)} mono />
                <Metric label="deployed" value={model.model_version.deployed_at} />
                <Chip
                  size="small"
                  sx={{ mt: 1 }}
                  color={model.consistent ? 'success' : 'error'}
                  label={model.consistent ? 'registry and engine agree' : 'VERSION MISMATCH'}
                />
              </>
            ) : (
              <Typography variant="body2" color="text.secondary">
                No active model version registered.
              </Typography>
            )}
          </CardContent>
        </Card>
      </Box>

      <Card sx={{ mb: 4 }}>
        <CardContent>
          <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', mb: 1, flexWrap: 'wrap', gap: 1 }}>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
              <AnalyticsIcon color="warning" />
              <Typography variant="subtitle1" sx={{ fontWeight: 700 }}>
                Population Stability Index
              </Typography>
            </Box>
            {mayMeasureDrift && (
              <Button size="small" variant="contained" onClick={measureDrift} disabled={measuring}>
                {measuring ? 'Measuring...' : 'Evaluate drift now'}
              </Button>
            )}
          </Box>
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 2 }}>
            Re-measures the live scored PD population against the shipped training baseline. Requires at least 200 scored
            applications in the window; thresholds 0.10 moderate, 0.25 critical.
          </Typography>
          {drift.length === 0 ? (
            <Typography variant="body2" color="text.secondary">
              No drift measurements recorded. {mayMeasureDrift ? 'Run the evaluation to create the first one.' : 'An ADMIN can run the evaluation.'}
            </Typography>
          ) : (
            <Table size="small">
              <TableHead>
                <TableRow>
                  <TableCell>Evaluated</TableCell>
                  <TableCell>Feature</TableCell>
                  <TableCell>PSI</TableCell>
                  <TableCell>Status</TableCell>
                  <TableCell>Sample</TableCell>
                  <TableCell>Model</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {drift.map((m) => (
                  <TableRow key={m.id}>
                    <TableCell>{m.evaluated_at}</TableCell>
                    <TableCell>{m.feature_name}</TableCell>
                    <TableCell sx={{ fontWeight: 700 }}>{m.psi_value.toFixed(4)}</TableCell>
                    <TableCell>
                      <Chip
                        size="small"
                        color={m.status === 'STABLE' ? 'success' : m.status === 'MODERATE' ? 'warning' : 'error'}
                        label={m.status}
                      />
                    </TableCell>
                    <TableCell>{m.sample_size}</TableCell>
                    <TableCell>{m.model_version}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardContent>
          <Typography variant="subtitle1" sx={{ fontWeight: 700, mb: 1 }}>
            Append-only Ledger (last {ledger.length} entries)
          </Typography>
          <Divider sx={{ mb: 2 }} />
          {ledger.length === 0 ? (
            <Typography variant="body2" color="text.secondary">
              The ledger is empty.
            </Typography>
          ) : (
            <Box sx={{ overflowX: 'auto' }}>
              <Table size="small">
                <TableHead>
                  <TableRow>
                    <TableCell>Recorded</TableCell>
                    <TableCell>Event</TableCell>
                    <TableCell>Actor</TableCell>
                    <TableCell>Application</TableCell>
                    <TableCell>Payload sha256</TableCell>
                    <TableCell>Entry hash</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {ledger.map((entry) => (
                    <TableRow key={entry.id}>
                      <TableCell sx={{ whiteSpace: 'nowrap' }}>{entry.recorded_at}</TableCell>
                      <TableCell>{entry.event_type}</TableCell>
                      <TableCell>{entry.actor}</TableCell>
                      <TableCell>
                        {entry.application_id ? (
                          <Link href={`/status/${entry.application_id}`} style={{ textDecoration: 'none' }}>
                            {short(entry.application_id)}
                          </Link>
                        ) : (
                          'platform'
                        )}
                      </TableCell>
                      <TableCell sx={{ fontFamily: 'monospace', fontSize: 11 }}>{short(entry.payload_sha256)}</TableCell>
                      <TableCell sx={{ fontFamily: 'monospace', fontSize: 11 }}>{short(entry.entry_hash)}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </Box>
          )}
        </CardContent>
      </Card>
    </Box>
  );
}

function Metric({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <Box sx={{ display: 'flex', justifyContent: 'space-between', gap: 2, py: 0.25 }}>
      <Typography variant="body2" color="text.secondary">
        {label}
      </Typography>
      <Typography variant="body2" sx={{ fontWeight: 600, fontFamily: mono ? 'monospace' : undefined }}>
        {value}
      </Typography>
    </Box>
  );
}

function short(value: string): string {
  if (!value) return '-';
  return value.length > 18 ? `${value.slice(0, 10)}...${value.slice(-6)}` : value;
}

function describe(err: unknown): string {
  if (err instanceof ApiError) return err.message;
  return 'One or more governance endpoints could not be read.';
}
