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
  Container,
  Divider,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  Typography,
} from '@mui/material';
import PostAddIcon from '@mui/icons-material/PostAdd';
import RefreshIcon from '@mui/icons-material/Refresh';
import OpenInNewIcon from '@mui/icons-material/OpenInNew';
import { api, ApiError } from '@/lib/api';
import type { Application, BorrowerApplicationPage } from '@/lib/api';
import { formatBpsToPercent, formatCentsToDollars } from '@/lib/formatters';
import { useAuth } from '@/lib/auth/AuthContext';

const POLL_INTERVAL_MS = 5000;

// The borrower's own ledger of cases. This is the session path to the same
// records the capability token opens: a person who applied while signed in does
// not have to keep a one-time code to find out what happened to their loan.
export default function MyApplicationsPage() {
  const { role, isAuthenticated, ready } = useAuth();
  const [page, setPage] = useState<BorrowerApplicationPage | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    try {
      const data = await api.myApplications();
      setPage(data);
      setError(null);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        setError('Your session has ended. Sign in again to read your applications.');
      } else {
        setError(err instanceof ApiError ? err.message : 'Could not read your applications.');
      }
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (ready && isAuthenticated && role === 'BORROWER') load();
    else if (ready) setLoading(false);
  }, [ready, isAuthenticated, role, load]);

  // Only keep polling while something is still moving through the saga, so a
  // page full of decided cases does not touch the API forever.
  useEffect(() => {
    const open = (page?.applications ?? []).some((a) => a.status === 'SUBMITTED' || a.status === 'IN_REVIEW');
    if (!open || !isAuthenticated || role !== 'BORROWER') return;
    const timer = setInterval(load, POLL_INTERVAL_MS);
    return () => clearInterval(timer);
  }, [page, isAuthenticated, role, load]);

  if (!ready || (loading && !page)) {
    return (
      <Container maxWidth="sm" sx={{ py: 10, textAlign: 'center' }}>
        <CircularProgress color="secondary" />
      </Container>
    );
  }

  if (!isAuthenticated || role !== 'BORROWER') {
    return (
      <Container maxWidth="sm" sx={{ py: 8 }}>
        <Alert
          severity="warning"
          sx={{ display: 'block', textAlign: 'left' }}
          action={
            <Button component={Link} href="/login" color="inherit" size="small">
              Sign In
            </Button>
          }
        >
          <Typography variant="subtitle2" sx={{ fontWeight: 700 }}>
            Your applications need a borrower session
          </Typography>
          <Typography variant="body2" color="text.secondary">
            {role === 'UNDERWRITER' || role === 'ADMIN'
              ? 'This account is a staff session. The credit desk reads the whole book from the underwriter queue.'
              : 'Sign in with the account you used to apply.'}
          </Typography>
        </Alert>
      </Container>
    );
  }

  if (error && !page) {
    return (
      <Container maxWidth="sm" sx={{ py: 8 }}>
        <Alert severity="error">{error}</Alert>
      </Container>
    );
  }

  const applications = page?.applications ?? [];

  return (
    <Container maxWidth="md" sx={{ py: 5 }}>
      <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', mb: 1, flexWrap: 'wrap', gap: 2 }}>
        <Typography variant="h5" sx={{ fontWeight: 800 }}>
          My Applications
        </Typography>
        <Box sx={{ display: 'flex', gap: 1 }}>
          <Button size="small" startIcon={loading ? <CircularProgress size={16} /> : <RefreshIcon />} onClick={load}>
            Refresh
          </Button>
          <Button size="small" component={Link} href="/apply" variant="contained" color="secondary" startIcon={<PostAddIcon />}>
            New application
          </Button>
        </Box>
      </Box>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
        Every case this account originated, with the credit file it was scored against. Open one to follow the decision
        as the underwriting saga works through it.
      </Typography>

      {error && (
        <Alert severity="warning" sx={{ mb: 3 }} onClose={() => setError(null)}>
          {error}
        </Alert>
      )}

      {page && !page.applicant_linked && (
        <Alert severity="info" sx={{ mb: 3, display: 'block', textAlign: 'left' }}>
          <Typography variant="subtitle2" sx={{ fontWeight: 700 }}>
            No credit record is bound to this account yet
          </Typography>
          <Typography variant="body2" color="text.secondary">
            {page.note ??
              'Applications are attached to the signed-in account when you submit one. Apply now and this list fills in.'}
          </Typography>
        </Alert>
      )}

      {page?.applicant_linked && applications.length === 0 && (
        <Alert severity="info">Your credit record has no applications on file.</Alert>
      )}

      {applications.map((app) => (
        <CaseCard key={app.id} app={app} />
      ))}
    </Container>
  );
}

function CaseCard({ app }: { app: Application }) {
  const decision = app.decision;
  const scoring = app.scoring_result;
  return (
    <Card variant="outlined" sx={{ mb: 3, borderTop: `4px solid ${accent(app.status)}` }}>
      <CardContent sx={{ p: 3, '&:last-child': { pb: 3 } }}>
        <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: 1.5 }}>
          <Box>
            <Typography variant="subtitle1" sx={{ fontWeight: 800, fontFamily: 'monospace' }}>
              {app.id}
            </Typography>
            <Typography variant="caption" color="text.secondary">
              submitted {formatTimestamp(app.created_at)}
            </Typography>
          </Box>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <StatusChip status={app.status} />
            {scoring?.risk_tier && <Chip size="small" variant="outlined" label={`${scoring.risk_tier} tier`} />}
          </Box>
        </Box>

        <Divider sx={{ my: 2 }} />

        <Table size="small">
          <TableBody>
            <Line label="Requested" value={`${formatCentsToDollars(app.requested_amount_cents)} over ${app.loan_term_months} months`} />
            <Line label="Purpose" value={app.loan_purpose.replace(/_/g, ' ')} />
            <Line label="Credit file" value={app.applicant ? `${app.applicant.first_name} ${app.applicant.last_name}` : app.applicant_id} />
            {scoring && (
              <Line
                label="Modelled limit"
                value={`${formatCentsToDollars(scoring.max_approved_limit_cents)} at ${formatBpsToPercent(scoring.recommended_apr_bps)}`}
              />
            )}
            {decision && (
              <Line
                label="Decision"
                value={`${decision.outcome} - ${formatCentsToDollars(decision.approved_limit_cents)} at ${formatBpsToPercent(
                  decision.approved_apr_bps,
                )} (${decision.decision_type})`}
              />
            )}
          </TableBody>
        </Table>

        <Button
          component={Link}
          href={`/status/${app.id}`}
          variant="contained"
          size="small"
          startIcon={<OpenInNewIcon />}
          sx={{ mt: 2 }}
        >
          Open decision detail
        </Button>
      </CardContent>
    </Card>
  );
}

function Line({ label, value }: { label: string; value: string }) {
  return (
    <TableRow>
      <TableCell sx={{ border: 0, color: 'text.secondary', width: '35%', pl: 0 }}>{label}</TableCell>
      <TableCell sx={{ border: 0, fontWeight: 600 }}>{value}</TableCell>
    </TableRow>
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

function accent(status: Application['status']): string {
  if (status === 'APPROVED') return '#10B981';
  if (status === 'DECLINED') return '#EF4444';
  if (status === 'IN_REVIEW') return '#F59E0B';
  return '#38BDF8';
}

function formatTimestamp(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toUTCString().replace('GMT', 'UTC');
}
