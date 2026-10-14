"use client";

import React, { useCallback, useEffect, useMemo, useState } from 'react';
import Link from 'next/link';
import {
  Alert,
  Box,
  Button,
  Card,
  CardContent,
  Chip,
  CircularProgress,
  FormControl,
  InputLabel,
  MenuItem,
  Select,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  ToggleButton,
  ToggleButtonGroup,
  Typography,
} from '@mui/material';
import RefreshIcon from '@mui/icons-material/Refresh';
import HistoryEduIcon from '@mui/icons-material/HistoryEdu';
import { api, ApiError } from '@/lib/api';
import type { Application, ApplicationPage, WorkflowState } from '@/lib/api';
import { formatCentsToDollars } from '@/lib/formatters';
import { useAuth } from '@/lib/auth/AuthContext';
import ApplicationDetail from '@/components/ApplicationDetail';
import UnderwriterReviewPanel from '@/components/UnderwriterReviewPanel';

const PAGE_SIZE = 25;
const POLL_INTERVAL_MS = 5000;

const STATUSES = ['SUBMITTED', 'IN_REVIEW', 'APPROVED', 'DECLINED'];
const TIERS = ['SUPER_PRIME', 'PRIME', 'NEAR_PRIME', 'SUBPRIME', 'HIGH_RISK'];

export default function UnderwriterQueue() {
  const { user, role, isAuthenticated, ready } = useAuth();
  const [statusFilter, setStatusFilter] = useState('');
  const [tierFilter, setTierFilter] = useState('');
  const [offset, setOffset] = useState(0);
  const [autoRefresh, setAutoRefresh] = useState(true);

  const [page, setPage] = useState<ApplicationPage | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [selected, setSelected] = useState<Application | null>(null);
  const [state, setState] = useState<WorkflowState | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const mayWorkQueue = role === 'UNDERWRITER' || role === 'ADMIN';

  const load = useCallback(async () => {
    if (!isAuthenticated || !mayWorkQueue) return;
    try {
      const data = await api.listApplications({
        status: statusFilter || undefined,
        tier: tierFilter || undefined,
        limit: PAGE_SIZE,
        offset,
      });
      setPage(data);
      setError(null);
      // A row that drops out of the filtered page deselects; one that is still
      // listed keeps its id, and the detail effect below re-reads it so a
      // finalized review does not leave a stale verdict in the panel. Functional
      // update keeps `selectedId` out of this callback's dependencies, otherwise
      // refreshing would re-create `load` and re-trigger its own effect.
      setSelectedId((current) => (current && data.applications.some((row) => row.id === current) ? current : null));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Could not read the underwriting queue.');
    } finally {
      setLoading(false);
    }
  }, [isAuthenticated, mayWorkQueue, statusFilter, tierFilter, offset]);

  useEffect(() => {
    if (!ready) return;
    load();
  }, [ready, load]);

  useEffect(() => {
    if (!autoRefresh || !isAuthenticated || !mayWorkQueue) return;
    const timer = setInterval(load, POLL_INTERVAL_MS);
    return () => clearInterval(timer);
  }, [autoRefresh, isAuthenticated, mayWorkQueue, load]);

  // The queue response carries one row per case, not the bureau payload: paging
  // twenty credit files into a list is waste, and the list never shows them. An
  // opened row is therefore re-read as a single record, which is also what keeps
  // the panel current on every poll.
  useEffect(() => {
    if (!selectedId) {
      setSelected(null);
      return;
    }
    let cancelled = false;
    api
      .getApplication(selectedId)
      .then((app) => !cancelled && setSelected(app))
      .catch((err) => !cancelled && setError(err instanceof ApiError ? err.message : 'Could not read the application.'));
    return () => {
      cancelled = true;
    };
  }, [selectedId, page]);

  // The query is only valid while the execution is open; a completed workflow
  // cannot be queried, so the stage line is dropped once the case is decided.
  useEffect(() => {
    if (!selected || selected.status === 'APPROVED' || selected.status === 'DECLINED') {
      setState(null);
      return;
    }
    let cancelled = false;
    api
      .workflowState(selected.id)
      .then((s) => !cancelled && setState(s))
      .catch(() => !cancelled && setState(null));
    return () => {
      cancelled = true;
    };
  }, [selected]);

  const totals = useMemo(() => {
    const counts = page?.by_status ?? {};
    return {
      pending: (counts['IN_REVIEW'] ?? 0) + (counts['SUBMITTED'] ?? 0),
      approved: counts['APPROVED'] ?? 0,
      declined: counts['DECLINED'] ?? 0,
      total: Object.values(counts).reduce((sum, n) => sum + n, 0),
    };
  }, [page]);

  if (!ready) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', py: 10 }}>
        <CircularProgress color="secondary" />
      </Box>
    );
  }

  if (!isAuthenticated || !mayWorkQueue) {
    return (
      <Box sx={{ py: 8 }}>
        <Alert
          severity="warning"
          action={
            <Button component={Link} href="/login" color="inherit" size="small">
              Sign In
            </Button>
          }
        >
          The underwriter queue requires an UNDERWRITER or ADMIN session.
          {role === 'BORROWER' ? ` ${user?.email ?? 'Your account'} is signed in as a borrower.` : ''}
        </Alert>
      </Box>
    );
  }

  return (
    <Box sx={{ py: 4, px: { xs: 2, md: 4 }, maxWidth: 1400, mx: 'auto' }}>
      <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', mb: 3, flexWrap: 'wrap', gap: 2 }}>
        <Typography variant="h5" sx={{ fontWeight: 800 }}>
          Underwriter Queue
        </Typography>
        <Box sx={{ display: 'flex', gap: 1, alignItems: 'center' }}>
          <ToggleButtonGroup
            exclusive
            size="small"
            value={autoRefresh ? 'on' : 'off'}
            onChange={(_, next) => setAutoRefresh(next === 'on')}
          >
            <ToggleButton value="on">Auto-refresh</ToggleButton>
            <ToggleButton value="off">Paused</ToggleButton>
          </ToggleButtonGroup>
          <Button
            size="small"
            startIcon={loading ? <CircularProgress size={16} /> : <RefreshIcon />}
            onClick={load}
            disabled={loading}
          >
            Refresh
          </Button>
          <Button
            size="small"
            component={Link}
            href="/dashboard/governance"
            startIcon={<HistoryEduIcon />}
            variant="outlined"
          >
            Governance
          </Button>
        </Box>
      </Box>

      {error && (
        <Alert severity="error" sx={{ mb: 3 }} onClose={() => setError(null)}>
          {error}
        </Alert>
      )}

      <Box sx={{ display: 'flex', gap: 2, mb: 3, flexWrap: 'wrap' }}>
        <StatChip label="Awaiting action" value={totals.pending} color="warning" />
        <StatChip label="Approved" value={totals.approved} color="success" />
        <StatChip label="Declined" value={totals.declined} color="error" />
        <StatChip label="Book size" value={totals.total} color="default" />
      </Box>

      <Box sx={{ display: 'flex', gap: 2, mb: 2, flexWrap: 'wrap' }}>
        <FormControl size="small" sx={{ minWidth: 180 }}>
          <InputLabel>Status</InputLabel>
          <Select
            value={statusFilter}
            label="Status"
            onChange={(e) => {
              setStatusFilter(e.target.value);
              setOffset(0);
            }}
          >
            <MenuItem value="">All statuses</MenuItem>
            {STATUSES.map((s) => (
              <MenuItem key={s} value={s}>
                {s}
              </MenuItem>
            ))}
          </Select>
        </FormControl>
        <FormControl size="small" sx={{ minWidth: 180 }}>
          <InputLabel>Risk Tier</InputLabel>
          <Select
            value={tierFilter}
            label="Risk Tier"
            onChange={(e) => {
              setTierFilter(e.target.value);
              setOffset(0);
            }}
          >
            <MenuItem value="">All tiers</MenuItem>
            {TIERS.map((t) => (
              <MenuItem key={t} value={t}>
                {t}
              </MenuItem>
            ))}
          </Select>
        </FormControl>
      </Box>

      <Box sx={{ display: 'flex', gap: 3, alignItems: 'flex-start', flexWrap: 'wrap' }}>
        <Card sx={{ flex: '1 1 520px', minWidth: 320 }}>
          <CardContent sx={{ p: 0 }}>
            <Table size="small">
              <TableHead>
                <TableRow>
                  <TableCell>Borrower</TableCell>
                  <TableCell>Amount</TableCell>
                  <TableCell>Tier</TableCell>
                  <TableCell>Score</TableCell>
                  <TableCell>Status</TableCell>
                  <TableCell>Latency</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {(page?.applications ?? []).map((app) => (
                  <TableRow
                    key={app.id}
                    hover
                    selected={selected?.id === app.id}
                    onClick={() => setSelectedId(app.id)}
                    onKeyDown={(e) => {
                      // The row is the only way to open a case, so it has to
                      // answer to the keyboard as well as the pointer.
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault();
                        setSelectedId(app.id);
                      }
                    }}
                    tabIndex={0}
                    aria-label={`${app.applicant ? `${app.applicant.first_name} ${app.applicant.last_name}` : 'Application'} ${app.status}, open for review`}
                    sx={{
                      cursor: 'pointer',
                      '&:focus-visible': {
                        outline: '2px solid',
                        outlineColor: 'primary.main',
                        outlineOffset: -2,
                      },
                    }}
                  >
                    <TableCell>
                      {app.applicant ? `${app.applicant.first_name} ${app.applicant.last_name}` : app.id.slice(0, 8)}
                    </TableCell>
                    <TableCell>{formatCentsToDollars(app.requested_amount_cents)}</TableCell>
                    <TableCell>{app.scoring_result?.risk_tier ?? '-'}</TableCell>
                    <TableCell>{app.scoring_result?.scorecard_points ?? '-'}</TableCell>
                    <TableCell>
                      <StatusChip status={app.status} />
                    </TableCell>
                    <TableCell>{app.scoring_result ? `${app.scoring_result.execution_time_micros} us` : '-'}</TableCell>
                  </TableRow>
                ))}
                {!loading && (page?.applications.length ?? 0) === 0 && (
                  <TableRow>
                    <TableCell colSpan={6}>
                      <Typography variant="body2" color="text.secondary" sx={{ py: 3 }}>
                        No applications match this filter. Run <code>make seed</code> to originate the demo book.
                      </Typography>
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          </CardContent>
          <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', p: 2 }}>
            <Typography variant="caption" color="text.secondary">
              {(page?.applications.length ?? 0) === 0
                ? 'no rows'
                : `rows ${offset + 1}-${offset + (page?.applications.length ?? 0)}`}
            </Typography>
            <Box sx={{ display: 'flex', gap: 1 }}>
              <Button size="small" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}>
                Previous
              </Button>
              <Button
                size="small"
                disabled={(page?.count ?? 0) < PAGE_SIZE}
                onClick={() => setOffset(offset + PAGE_SIZE)}
              >
                Next
              </Button>
            </Box>
          </Box>
        </Card>

        <Box sx={{ flex: '1 1 460px', minWidth: 320, display: 'flex', flexDirection: 'column', gap: 3 }}>
          {selected ? (
            <>
              {selected.status === 'IN_REVIEW' && (
                <UnderwriterReviewPanel application={selected} onSubmitted={() => load()} />
              )}
              <ApplicationDetail app={selected} workflowState={state} />
            </>
          ) : (
            <Card variant="outlined">
              <CardContent>
                <Typography variant="body2" color="text.secondary">
                  Select an application to inspect its bureau file, model output and TreeSHAP attributions.
                </Typography>
              </CardContent>
            </Card>
          )}
        </Box>
      </Box>
    </Box>
  );
}

function StatChip({
  label,
  value,
  color,
}: {
  label: string;
  value: number;
  color: 'default' | 'warning' | 'success' | 'error';
}) {
  return (
    <Card variant="outlined" sx={{ minWidth: 150, flex: '0 1 auto' }}>
      <CardContent sx={{ py: 1.5, px: 2, '&:last-child': { pb: 1.5 } }}>
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
          {label}
        </Typography>
        <Typography variant="h5" sx={{ fontWeight: 800, color: color === 'default' ? 'text.primary' : `${color}.main` }}>
          {value}
        </Typography>
      </CardContent>
    </Card>
  );
}

function StatusChip({ status }: { status: Application['status'] }) {
  const color =
    status === 'APPROVED'
      ? 'success'
      : status === 'DECLINED'
        ? 'error'
        : status === 'IN_REVIEW'
          ? 'warning'
          : status === 'SUBMITTED'
            ? 'info'
            : 'default';
  return <Chip size="small" label={status} color={color as 'default'} />;
}
