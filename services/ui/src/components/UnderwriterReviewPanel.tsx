"use client";

import React, { useEffect, useMemo, useState } from 'react';
import {
  Alert,
  Box,
  Button,
  Card,
  CardContent,
  Divider,
  TextField,
  ToggleButton,
  ToggleButtonGroup,
  Typography,
} from '@mui/material';
import type { Application } from '@/lib/api';
import { api, ApiError } from '@/lib/api';
import { formatBpsToPercent, formatCentsToDollars } from '@/lib/formatters';

// The signal is accepted asynchronously: core returns 202 and the saga writes the
// verdict, so the panel reports "submitted" rather than implying the case is closed.
export default function UnderwriterReviewPanel({
  application,
  onSubmitted,
}: {
  application: Application;
  onSubmitted: () => void;
}) {
  const scoring = application.scoring_result;
  const suggestedLimitCents = scoring?.max_approved_limit_cents ?? application.requested_amount_cents;
  const suggestedAprBps = scoring?.recommended_apr_bps ?? 1240;

  const [decision, setDecision] = useState<'APPROVE' | 'DECLINE'>('APPROVE');
  const [limitDollars, setLimitDollars] = useState(Math.round(suggestedLimitCents / 100));
  const [aprPercent, setAprPercent] = useState(Number((suggestedAprBps / 100).toFixed(2)));
  const [notes, setNotes] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [accepted, setAccepted] = useState(false);

  // Prefill follows the case, not the first case the panel ever saw.
  useEffect(() => {
    setLimitDollars(Math.round(suggestedLimitCents / 100));
    setAprPercent(Number((suggestedAprBps / 100).toFixed(2)));
    setAccepted(false);
    setError(null);
    setFieldErrors({});
  }, [application.id, suggestedLimitCents, suggestedAprBps]);

  const approvedLimitCents = useMemo(() => Math.round(limitDollars * 100), [limitDollars]);
  const approvedAprBps = useMemo(() => Math.round(aprPercent * 100), [aprPercent]);

  const submit = async () => {
    setBusy(true);
    setError(null);
    setFieldErrors({});
    try {
      await api.review(application.id, {
        decision,
        approved_limit_cents: decision === 'APPROVE' ? approvedLimitCents : 0,
        approved_apr_bps: decision === 'APPROVE' ? approvedAprBps : 0,
        notes: notes.trim(),
      });
      setAccepted(true);
      onSubmitted();
    } catch (err) {
      if (err instanceof ApiError) {
        setError(err.message);
        setFieldErrors(err.fieldErrors);
      } else {
        setError('The review signal could not be delivered.');
      }
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card sx={{ border: '2px solid #F59E0B' }}>
      <CardContent sx={{ p: 3 }}>
        <Typography variant="h6" sx={{ fontWeight: 800, mb: 0.5 }}>
          Underwriter Decision
        </Typography>
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 2, fontFamily: 'monospace' }}>
          {application.id}
        </Typography>

        {scoring ? (
          <Alert severity="info" sx={{ mb: 2 }}>
            Model recommends <strong>{scoring.recommended_outcome}</strong>: {scoring.risk_tier},{' '}
            {scoring.scorecard_points} points, PD {formatBpsToPercent(scoring.pd_basis_points)}, max limit{' '}
            {formatCentsToDollars(scoring.max_approved_limit_cents)} at {formatBpsToPercent(scoring.recommended_apr_bps)}{' '}
            APR. Your override is recorded as a MANUAL_OVERRIDE with your user id.
          </Alert>
        ) : (
          <Alert severity="warning" sx={{ mb: 2 }}>
            This case has no persisted model output, so there is no recommendation to anchor the override.
          </Alert>
        )}

        {error && (
          <Alert severity="error" sx={{ mb: 2 }} onClose={() => setError(null)}>
            {error}
          </Alert>
        )}
        {accepted && (
          <Alert severity="success" sx={{ mb: 2 }}>
            Decision received by the saga. The application status updates as soon as the workflow records it.
          </Alert>
        )}

        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
          <ToggleButtonGroup
            exclusive
            value={decision}
            onChange={(_, next) => next && setDecision(next)}
            disabled={busy || accepted}
          >
            <ToggleButton value="APPROVE" sx={{ flex: 1 }}>
              Approve
            </ToggleButton>
            <ToggleButton value="DECLINE" sx={{ flex: 1 }}>
              Decline
            </ToggleButton>
          </ToggleButtonGroup>

          {decision === 'APPROVE' && (
            <Box sx={{ display: 'flex', gap: 2 }}>
              <TextField
                label="Approved Limit ($)"
                type="number"
                fullWidth
                value={limitDollars}
                onChange={(e) => setLimitDollars(Number(e.target.value))}
                error={!!fieldErrors.approved_limit_cents}
                helperText={fieldErrors.approved_limit_cents || formatCentsToDollars(approvedLimitCents)}
                inputProps={{ min: 0, step: 100 }}
              />
              <TextField
                label="Approved APR (%)"
                type="number"
                fullWidth
                value={aprPercent}
                onChange={(e) => setAprPercent(Number(e.target.value))}
                error={!!fieldErrors.approved_apr_bps}
                helperText={fieldErrors.approved_apr_bps || `${approvedAprBps} bps`}
                inputProps={{ min: 1, max: 36, step: 0.01 }}
              />
            </Box>
          )}

          <TextField
            label="Decision notes"
            multiline
            minRows={3}
            value={notes}
            onChange={(e) => setNotes(e.target.value)}
            error={!!fieldErrors.notes}
            helperText={fieldErrors.notes || 'Recorded on the decision row and in the audit ledger. Max 2000 characters.'}
            inputProps={{ maxLength: 2000 }}
          />

          <Divider />

          <Button
            variant="contained"
            color={decision === 'APPROVE' ? 'success' : 'error'}
            onClick={submit}
            disabled={busy || accepted}
          >
            {busy ? 'Signalling saga...' : accepted ? 'Decision submitted' : 'Send decision to underwriting saga'}
          </Button>
        </Box>
      </CardContent>
    </Card>
  );
}
