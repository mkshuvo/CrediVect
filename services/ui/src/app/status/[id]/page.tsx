"use client";

import React, { useCallback, useEffect, useRef, useState } from 'react';
import { useParams } from 'next/navigation';
import {
  Container,
  Card,
  CardContent,
  Typography,
  Box,
  Chip,
  Button,
  Alert,
  CircularProgress,
  Divider,
  Table,
  TableBody,
  TableCell,
  TableRow,
} from '@mui/material';
import CheckCircleIcon from '@mui/icons-material/CheckCircle';
import HourglassEmptyIcon from '@mui/icons-material/HourglassEmpty';
import CancelIcon from '@mui/icons-material/Cancel';
import DescriptionIcon from '@mui/icons-material/Description';
import { api, ApiError } from '@/lib/api';
import type { AdverseNotice, Application, WorkflowState } from '@/lib/api';
import { formatBpsToPercent, formatCentsToDollars } from '@/lib/formatters';
import ShapWaterfall from '@/components/ShapWaterfall';

const POLL_INTERVAL_MS = 2500;

export default function ApplicationStatusPage() {
  const { id } = useParams<{ id: string }>();
  const [app, setApp] = useState<Application | null>(null);
  const [state, setState] = useState<WorkflowState | null>(null);
  const [notice, setNotice] = useState<AdverseNotice | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loaded, setLoaded] = useState(false);
  // Held in a ref so the interval callback never closes over a stale id, and a
  // response that arrives after unmount cannot set state.
  const alive = useRef(true);

  const load = useCallback(async () => {
    if (!id) return;
    try {
      const data = await api.getApplication(id);
      if (!alive.current) return;
      setApp(data);
      setError(null);
    } catch (err) {
      if (!alive.current) return;
      setError(err instanceof ApiError ? err.message : 'Unable to read the application.');
    } finally {
      if (alive.current) setLoaded(true);
    }
  }, [id]);

  useEffect(() => {
    alive.current = true;
    load();
    const timer = setInterval(load, POLL_INTERVAL_MS);
    return () => {
      alive.current = false;
      clearInterval(timer);
    };
  }, [load]);

  // The workflow query is only meaningful while the saga is still running; a
  // completed execution is no longer queryable.
  useEffect(() => {
    if (!app || app.status === 'APPROVED' || app.status === 'DECLINED' || app.status === 'CANCELLED') {
      setState(null);
      return;
    }
    let cancelled = false;
    api
      .workflowState(app.id)
      .then((s) => !cancelled && setState(s))
      .catch(() => !cancelled && setState(null));
    return () => {
      cancelled = true;
    };
  }, [app]);

  useEffect(() => {
    if (!app || (app.status !== 'DECLINED' && app.status !== 'IN_REVIEW')) {
      setNotice(null);
      return;
    }
    let cancelled = false;
    api
      .adverseAction(app.id)
      .then((n) => !cancelled && setNotice(n))
      .catch(() => !cancelled && setNotice(null));
    return () => {
      cancelled = true;
    };
  }, [app]);

  if (!loaded) {
    return (
      <Container maxWidth="sm" sx={{ py: 10, textAlign: 'center' }}>
        <CircularProgress color="secondary" />
        <Typography variant="body1" sx={{ mt: 2 }}>
          Retrieving underwriting decision...
        </Typography>
      </Container>
    );
  }

  if (error && !app) {
    return (
      <Container maxWidth="sm" sx={{ py: 10 }}>
        <Alert severity="error">{error}</Alert>
      </Container>
    );
  }

  if (!app) {
    return (
      <Container maxWidth="sm" sx={{ py: 10 }}>
        <Alert severity="warning">No application found for ID: {id}</Alert>
      </Container>
    );
  }

  const scoring = app.scoring_result;

  return (
    <Container maxWidth="md" sx={{ py: 6 }}>
      {error && (
        <Alert severity="warning" sx={{ mb: 3 }} onClose={() => setError(null)}>
          {error}
        </Alert>
      )}

      {/* 1. APPROVAL CARD */}
      {app.status === 'APPROVED' && (
        <Card sx={{ borderTop: '6px solid #10B981', mb: 4 }}>
          <CardContent sx={{ p: 4, textAlign: 'center' }}>
            <CheckCircleIcon sx={{ fontSize: 64, color: 'success.main', mb: 1 }} />
            <Typography variant="h4" sx={{ fontWeight: 800, color: 'success.dark', mb: 1 }}>
              Congratulations! Your Loan is Approved
            </Typography>
            <Typography variant="body1" color="text.secondary" sx={{ mb: 3 }}>
              Application ID: {app.id}
            </Typography>

            <Box
              sx={{ display: 'flex', justifyContent: 'center', gap: 4, mb: 3, p: 3, bgcolor: '#F0FDF4', borderRadius: 2 }}
            >
              <Box>
                <Typography variant="caption" color="text.secondary">
                  APPROVED CREDIT LIMIT
                </Typography>
                <Typography variant="h5" sx={{ fontWeight: 800, color: 'success.dark' }}>
                  {formatCentsToDollars(app.decision?.approved_limit_cents ?? scoring?.max_approved_limit_cents ?? 0)}
                </Typography>
              </Box>
              <Divider orientation="vertical" flexItem />
              <Box>
                <Typography variant="caption" color="text.secondary">
                  APPROVED APR
                </Typography>
                <Typography variant="h5" sx={{ fontWeight: 800, color: 'success.dark' }}>
                  {formatBpsToPercent(app.decision?.approved_apr_bps ?? scoring?.recommended_apr_bps ?? 0)}
                </Typography>
              </Box>
              <Divider orientation="vertical" flexItem />
              <Box>
                <Typography variant="caption" color="text.secondary">
                  TERM
                </Typography>
                <Typography variant="h5" sx={{ fontWeight: 800, color: 'success.dark' }}>
                  {app.loan_term_months} Months
                </Typography>
              </Box>
            </Box>

            <Box sx={{ display: 'flex', justifyContent: 'center', alignItems: 'center', gap: 1 }}>
              <Chip
                size="small"
                color={app.decision?.decision_type === 'MANUAL_OVERRIDE' ? 'warning' : 'success'}
                label={
                  app.decision?.decision_type === 'MANUAL_OVERRIDE'
                    ? 'Approved by underwriter review'
                    : 'Approved by automated underwriting'
                }
              />
              <Typography variant="caption" color="text.secondary">
                Decisioned {formatTimestamp(app.decision?.decisioned_at ?? app.updated_at)}
              </Typography>
            </Box>
            {app.decision?.decision_notes && (
              <Alert severity="success" sx={{ mt: 2, textAlign: 'left' }}>
                <strong>Underwriter note:</strong> {app.decision.decision_notes}
              </Alert>
            )}
          </CardContent>
        </Card>
      )}

      {/* 2. IN REVIEW CARD (Human-in-the-Loop) */}
      {app.status === 'IN_REVIEW' && (
        <Card sx={{ borderTop: '6px solid #F59E0B', mb: 4 }}>
          <CardContent sx={{ p: 4, textAlign: 'center' }}>
            <HourglassEmptyIcon sx={{ fontSize: 64, color: 'warning.main', mb: 1 }} />
            <Typography variant="h4" sx={{ fontWeight: 800, color: 'warning.dark', mb: 1 }}>
              Application Under Credit Review
            </Typography>
            <Typography variant="body1" color="text.secondary" sx={{ mb: 3 }}>
              The scoring model referred your file to the credit desk instead of deciding it automatically.
            </Typography>
            <Alert severity="warning" sx={{ maxWidth: 640, mx: 'auto', textAlign: 'left' }}>
              <strong>{scoring?.risk_tier ?? 'Referred'} review tier:</strong> the underwriting saga is parked on a
              72-hour human review timer. An underwriter decision finalizes it immediately; if no one acts within the
              window the case expires and is declined on the recorded reasons.
              {state?.waiting_since && (
                <>
                  <br />
                  <Box component="span" sx={{ fontFamily: 'monospace', fontSize: 12 }}>
                    parked since {formatTimestamp(state.waiting_since)}
                  </Box>
                </>
              )}
            </Alert>
          </CardContent>
        </Card>
      )}

      {/* 3. DECLINED CARD (FCRA Adverse Action Notice) */}
      {app.status === 'DECLINED' && (
        <Card sx={{ borderTop: '6px solid #EF4444', mb: 4 }}>
          <CardContent sx={{ p: 4 }}>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 2, mb: 2 }}>
              <CancelIcon sx={{ fontSize: 48, color: 'error.main' }} />
              <Box>
                <Typography variant="h5" sx={{ fontWeight: 800, color: 'error.dark' }}>
                  Notice of Adverse Action
                </Typography>
                <Typography variant="body2" color="text.secondary">
                  Formal disclosure pursuant to the Fair Credit Reporting Act (15 U.S.C. 1681m)
                </Typography>
              </Box>
            </Box>

            <Divider sx={{ my: 2 }} />

            <Typography variant="body2" sx={{ mb: 2 }}>
              Thank you for your interest in credit with CrediVect. We regret to inform you that we are unable to
              approve your application at this time based on our quantitative credit scoring models.
            </Typography>

            {notice && (
              <Box sx={{ mb: 3 }}>
                <Typography variant="subtitle2" sx={{ fontWeight: 700 }}>
                  Creditor
                </Typography>
                <Typography variant="body2" color="text.secondary">
                  {notice.creditor.name} - {notice.creditor.address} - {notice.creditor.phone}
                </Typography>
                <Typography variant="subtitle2" sx={{ fontWeight: 700, mt: 1.5 }}>
                  Consumer reporting agency whose report was used
                </Typography>
                <Typography variant="body2" color="text.secondary">
                  {notice.reporting_agency.name} - {notice.reporting_agency.address} - {notice.reporting_agency.phone}
                </Typography>
              </Box>
            )}

            <Typography variant="subtitle2" sx={{ fontWeight: 700, mb: 1 }}>
              Key Factors Contributing to this Decision (Ranked by Impact):
            </Typography>

            <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5, mb: 3 }}>
              {(notice?.reasons ?? scoring?.adverse_action_reasons ?? []).map((reason) => (
                <Card key={reason.reason_code} variant="outlined" sx={{ p: 2, bgcolor: '#FEF2F2' }}>
                  <Typography variant="body2" sx={{ fontWeight: 700, color: 'error.dark' }}>
                    {reason.priority_rank}. [{reason.reason_code}] {reason.description}
                  </Typography>
                </Card>
              ))}
              {(notice?.reasons ?? scoring?.adverse_action_reasons ?? []).length === 0 && (
                <Typography variant="body2" color="text.secondary">
                  No reason codes were recorded for this decision.
                </Typography>
              )}
            </Box>

            <Button
              component="a"
              href={api.adverseActionNoticeUrl(app.id)}
              variant="outlined"
              color="error"
              startIcon={<DescriptionIcon />}
              target="_blank"
            >
              Download Adverse Action Notice (plain text)
            </Button>
          </CardContent>
        </Card>
      )}

      {app.status === 'SUBMITTED' && (
        <Alert severity="info" sx={{ mb: 4 }}>
          Your application is queued in the durable underwriting saga. This page refreshes automatically.
        </Alert>
      )}

      {app.status === 'CANCELLED' && (
        <Alert severity="info" sx={{ mb: 4 }}>
          This application was withdrawn and will not be decided.
        </Alert>
      )}

      {/* Scoring transparency: the numbers the model actually returned */}
      {scoring && (
        <Card variant="outlined" sx={{ mb: 4 }}>
          <CardContent>
            <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', mb: 2 }}>
              <Typography variant="h6" sx={{ fontWeight: 700 }}>
                Credit Decision Detail
              </Typography>
              <Chip size="small" color="primary" label={`${scoring.risk_tier} tier`} />
            </Box>
            <DetailTable
              rows={[
                ['CrediVect scorecard', `${scoring.scorecard_points} (range 300-850)`],
                ['Bureau score', app.bureau_report ? `${app.bureau_report.bureau_score}` : 'not pulled'],
                ['Modelled probability of default', `${scoring.calibrated_pd} (${scoring.pd_basis_points} bps)`],
                ['Engine recommendation', scoring.recommended_outcome],
                ['Max approved limit', formatCentsToDollars(scoring.max_approved_limit_cents)],
                ['Priced APR at that limit', formatBpsToPercent(scoring.recommended_apr_bps)],
                ['Scoring latency', `${scoring.execution_time_micros} us`],
                ['Model version', scoring.model_version],
                ['Reported by bureau', app.bureau_report ? `${app.bureau_report.bureau_name}` : 'n/a'],
              ]}
            />
          </CardContent>
        </Card>
      )}

      {scoring?.feature_attributions && scoring.feature_attributions.length > 0 && (
        <ShapWaterfall attributions={scoring.feature_attributions} />
      )}
    </Container>
  );
}

// A two-column label/value table without inventing a layout primitive: the
// decision detail is read-only text of very uneven length.
function DetailTable({ rows }: { rows: [string, string][] }) {
  return (
    <Table size="small">
      <TableBody>
        {rows.map(([label, value]) => (
          <TableRow key={label}>
            <TableCell sx={{ border: 0, color: 'text.secondary', width: '45%' }}>{label}</TableCell>
            <TableCell sx={{ border: 0, fontWeight: 600 }}>{value}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

function formatTimestamp(value: string | undefined): string {
  if (!value) return 'unknown';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toUTCString().replace('GMT', 'UTC');
}
