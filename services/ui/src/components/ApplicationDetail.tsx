"use client";

import React, { useState } from 'react';
import {
  Alert,
  Box,
  Button,
  Card,
  CardContent,
  Chip,
  Divider,
  Table,
  TableBody,
  TableCell,
  TableRow,
  Typography,
} from '@mui/material';
import DescriptionIcon from '@mui/icons-material/Description';
import type { Application, WorkflowState } from '@/lib/api';
import { ApiError, downloadAdverseActionNotice } from '@/lib/api';
import { formatBpsToPercent, formatCentsToDollars } from '@/lib/formatters';
import ShapWaterfall from '@/components/ShapWaterfall';

const tierColor: Record<string, 'success' | 'info' | 'warning' | 'error'> = {
  SUPER_PRIME: 'success',
  PRIME: 'success',
  NEAR_PRIME: 'warning',
  SUBPRIME: 'error',
  HIGH_RISK: 'error',
};

export default function ApplicationDetail({
  app,
  workflowState,
}: {
  app: Application;
  workflowState: WorkflowState | null;
}) {
  const scoring = app.scoring_result;
  const bureau = app.bureau_report;
  const [noticeError, setNoticeError] = useState<string | null>(null);
  const [downloading, setDownloading] = useState(false);

  const saveNotice = async () => {
    setDownloading(true);
    setNoticeError(null);
    try {
      await downloadAdverseActionNotice(app.id);
    } catch (err) {
      setNoticeError(err instanceof ApiError ? err.message : 'The notice could not be downloaded.');
    } finally {
      setDownloading(false);
    }
  };

  return (
    <Card variant="outlined" sx={{ bgcolor: '#F8FAFC' }}>
      <CardContent sx={{ p: 3 }}>
        <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 2, mb: 1 }}>
          <Box>
            <Typography variant="h6" sx={{ fontWeight: 700 }}>
              {app.applicant ? `${app.applicant.first_name} ${app.applicant.last_name}` : 'Applicant'}
            </Typography>
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block', fontFamily: 'monospace' }}>
              {app.id}
            </Typography>
          </Box>
          <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap', justifyContent: 'flex-end' }}>
            <Chip size="small" label={app.status} color={statusColor(app.status)} />
            {scoring && (
              <Chip size="small" label={scoring.risk_tier} color={tierColor[scoring.risk_tier] ?? 'default'} />
            )}
          </Box>
        </Box>

        {workflowState && (
          <Alert severity="info" sx={{ mb: 1 }}>
            Saga stage <strong>{workflowState.stage}</strong>
            {workflowState.waiting_since ? `, parked since ${workflowState.waiting_since}` : ''}
          </Alert>
        )}

        <Divider sx={{ my: 2 }} />

        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
          <Section title="Request">
            <Row label="Requested amount" value={formatCentsToDollars(app.requested_amount_cents)} />
            <Row label="Term" value={`${app.loan_term_months} months`} />
            <Row label="Purpose" value={app.loan_purpose} />
            <Row label="Monthly income" value={formatCentsToDollars(app.applicant?.monthly_income_cents ?? 0)} />
            <Row label="Employment" value={app.applicant?.employment_status ?? 'n/a'} />
            <Row label="Submitted" value={app.created_at} />
            <Row label="Record version (OCC)" value={String(app.version)} />
          </Section>

          {bureau ? (
            <Section title={`Bureau file - ${bureau.bureau_name}`}>
              <Row label="Bureau score" value={String(bureau.bureau_score)} />
              <Row label="Total monthly debt" value={formatCentsToDollars(bureau.total_debt_cents)} />
              <Row
                label="Revolving balance / limit"
                value={`${formatCentsToDollars(bureau.revolving_balance_cents)} / ${formatCentsToDollars(
                  bureau.revolving_limit_cents,
                )}`}
              />
              <Row label="Utilization" value={formatBpsToPercent(bureau.revolving_utilization_bps)} />
              <Row label="Delinquencies (24m)" value={String(bureau.delinquencies_24m)} />
              <Row label="Inquiries (6m)" value={String(bureau.inquiries_6m)} />
              <Row label="Trades total / open" value={`${bureau.total_trades} / ${bureau.open_trades}`} />
              <Row label="Oldest trade" value={`${bureau.age_of_oldest_trade_months} months`} />
              <Row label="Bankruptcy flag" value={bureau.bankruptcy_flag ? 'PRESENT (policy knockout)' : 'none'} />
              <Row label="File valid time" value={bureau.valid_time} />
            </Section>
          ) : (
            <Typography variant="body2" color="text.secondary">
              No bureau report has been attached yet; the saga pulls the file before scoring.
            </Typography>
          )}

          {scoring ? (
            <>
              <Section title="Model output">
                <Row label="Scorecard points" value={`${scoring.scorecard_points} (300-850)`} />
                <Row label="Calibrated PD" value={`${scoring.calibrated_pd} (${scoring.pd_basis_points} bps)`} />
                <Row label="Raw ML logit" value={scoring.raw_ml_logit.toFixed(6)} />
                <Row label="Recommendation" value={scoring.recommended_outcome} />
                <Row label="Max approved limit" value={formatCentsToDollars(scoring.max_approved_limit_cents)} />
                <Row label="Recommended APR" value={formatBpsToPercent(scoring.recommended_apr_bps)} />
                <Row label="Scoring latency" value={`${scoring.execution_time_micros} us`} />
                <Row label="Model version" value={scoring.model_version} />
              </Section>

              {scoring.adverse_action_reasons.length > 0 && (
                <Box>
                  <Typography variant="subtitle2" sx={{ fontWeight: 700, mb: 1 }}>
                    Reason codes
                  </Typography>
                  <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.75 }}>
                    {scoring.adverse_action_reasons.map((reason) => (
                      <Typography key={reason.reason_code} variant="body2">
                        {reason.priority_rank}. [{reason.reason_code}] {reason.description}
                      </Typography>
                    ))}
                  </Box>
                </Box>
              )}

              {scoring.feature_attributions.length > 0 && <ShapWaterfall attributions={scoring.feature_attributions} />}
            </>
          ) : (
            <Typography variant="body2" color="text.secondary">
              Not scored yet.
            </Typography>
          )}

          {app.decision && (
            <Section title="Final decision">
              <Row label="Outcome" value={app.decision.outcome} />
              <Row label="Type" value={app.decision.decision_type} />
              <Row
                label="Approved terms"
                value={
                  app.decision.approved_limit_cents > 0
                    ? `${formatCentsToDollars(app.decision.approved_limit_cents)} at ${formatBpsToPercent(
                        app.decision.approved_apr_bps,
                      )}`
                    : 'none'
                }
              />
              {app.decision.underwriter_id && <Row label="Underwriter" value={app.decision.underwriter_id} />}
              {app.decision.decision_notes && <Row label="Notes" value={app.decision.decision_notes} />}
              <Row label="Decided" value={app.decision.decisioned_at} />
            </Section>
          )}

          {(app.status === 'DECLINED' || app.status === 'IN_REVIEW') && (
            <Box sx={{ mt: 1 }}>
              <Button
                onClick={saveNotice}
                disabled={downloading}
                startIcon={<DescriptionIcon />}
                variant="outlined"
                size="small"
              >
                {downloading ? 'Preparing notice...' : 'Adverse action notice'}
              </Button>
              {noticeError && (
                <Alert severity="error" sx={{ mt: 1 }}>
                  {noticeError}
                </Alert>
              )}
            </Box>
          )}
        </Box>
      </CardContent>
    </Card>
  );
}

function statusColor(status: Application['status']): 'default' | 'info' | 'warning' | 'success' | 'error' {
  switch (status) {
    case 'SUBMITTED':
      return 'info';
    case 'IN_REVIEW':
      return 'warning';
    case 'APPROVED':
      return 'success';
    case 'DECLINED':
      return 'error';
    default:
      return 'default';
  }
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <Box>
      <Typography variant="subtitle2" sx={{ fontWeight: 700, mb: 0.5 }}>
        {title}
      </Typography>
      <Table size="small">
        <TableBody>{children}</TableBody>
      </Table>
    </Box>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <TableRow>
      <TableCell sx={{ border: 0, py: 0.25, color: 'text.secondary', width: '45%' }}>{label}</TableCell>
      <TableCell sx={{ border: 0, py: 0.25, fontWeight: 600 }}>{value}</TableCell>
    </TableRow>
  );
}

function Alertish({ children }: { children: React.ReactNode }) {
  return (
    <Typography variant="body2" sx={{ bgcolor: '#EFF6FF', border: '1px solid #BFDBFE', borderRadius: 1, px: 1.5, py: 1 }}>
      {children}
    </Typography>
  );
}
