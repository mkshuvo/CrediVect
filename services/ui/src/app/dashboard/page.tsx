"use client";

import React, { useEffect, useState } from 'react';
import {
  Container,
  Typography,
  Card,
  CardContent,
  Table,
  TableHead,
  TableRow,
  TableCell,
  TableBody,
  Chip,
  Button,
  Box,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  Alert
} from '@mui/material';
import RateReviewIcon from '@mui/icons-material/RateReview';
import CheckCircleIcon from '@mui/icons-material/CheckCircle';
import CancelIcon from '@mui/icons-material/Cancel';
import { listApplications, reviewDecision } from '@/lib/api';
import { formatCentsToDollars, formatBpsToPercent } from '@/lib/formatters';
import ShapWaterfall from '@/components/ShapWaterfall';

export default function UnderwriterWorkspace() {
  const [apps, setApps] = useState<any[]>([]);
  const [selectedApp, setSelectedApp] = useState<any | null>(null);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [limitDollars, setLimitDollars] = useState(8000);
  const [aprPercent, setAprPercent] = useState(12.4);
  const [notes, setNotes] = useState('Approved based on steady secondary freelance cash flow.');

  const load = async () => {
    try {
      const data = await listApplications();
      setApps(data);
    } catch (e) {
      console.error(e);
    }
  };

  useEffect(() => {
    load();
    const interval = setInterval(load, 4000);
    return () => clearInterval(interval);
  }, []);

  const handleOpenReview = (app: any) => {
    setSelectedApp(app);
    setLimitDollars(app.scoring_result?.max_approved_limit_cents ? app.scoring_result.max_approved_limit_cents / 100 : 7500);
    setAprPercent(app.scoring_result?.recommended_apr_bps ? app.scoring_result.recommended_apr_bps / 100 : 12.4);
    setDialogOpen(true);
  };

  const handleExecuteDecision = async (outcome: 'APPROVE' | 'DECLINE') => {
    if (!selectedApp) return;
    try {
      const limitCents = Math.round(limitDollars * 100);
      const aprBps = Math.round(aprPercent * 100);
      await reviewDecision(selectedApp.id, outcome, limitCents, aprBps, notes);
      setDialogOpen(false);
      load();
    } catch (e: any) {
      alert(e.message);
    }
  };

  return (
    <Container maxWidth="xl" sx={{ py: 5 }}>
      <Box sx={{ mb: 4 }}>
        <Typography variant="h4" sx={{ fontWeight: 800, mb: 1 }}>
          Risk Officer & Underwriter Workspace
        </Typography>
        <Typography variant="body1" color="text.secondary">
          Human-in-the-Loop Review Queue: Evaluate Near-Prime edge cases, review SHAP waterfals, and fire Temporal Signals.
        </Typography>
      </Box>

      <Card>
        <CardContent sx={{ p: 0 }}>
          <Table>
            <TableHead sx={{ bgcolor: '#F1F5F9' }}>
              <TableRow>
                <TableCell sx={{ fontWeight: 700 }}>Application ID</TableCell>
                <TableCell sx={{ fontWeight: 700 }}>Borrower</TableCell>
                <TableCell sx={{ fontWeight: 700 }}>Requested</TableCell>
                <TableCell sx={{ fontWeight: 700 }}>WoE Score</TableCell>
                <TableCell sx={{ fontWeight: 700 }}>Probability of Default (PD)</TableCell>
                <TableCell sx={{ fontWeight: 700 }}>Risk Tier</TableCell>
                <TableCell sx={{ fontWeight: 700 }}>Status</TableCell>
                <TableCell sx={{ fontWeight: 700 }}>Action</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {apps.map((app) => (
                <TableRow key={app.id} hover>
                  <TableCell sx={{ fontFamily: 'monospace', fontSize: 13 }}>
                    {app.id.substring(0, 18)}...
                  </TableCell>
                  <TableCell>
                    <Typography variant="body2" sx={{ fontWeight: 600 }}>
                      {app.applicant?.first_name} {app.applicant?.last_name}
                    </Typography>
                    <Typography variant="caption" color="text.secondary">
                      {app.applicant?.email}
                    </Typography>
                  </TableCell>
                  <TableCell sx={{ fontWeight: 600 }}>
                    {formatCentsToDollars(app.requested_amount_cents)}
                  </TableCell>
                  <TableCell sx={{ fontWeight: 700 }}>
                    {app.scoring_result?.scorecard_points || '—'}
                  </TableCell>
                  <TableCell sx={{ fontWeight: 600, color: 'text.secondary' }}>
                    {app.scoring_result?.calibrated_pd ? `${(parseFloat(app.scoring_result.calibrated_pd) * 100).toFixed(2)}%` : '—'}
                  </TableCell>
                  <TableCell>
                    <Chip
                      label={app.scoring_result?.risk_tier || 'UNKNOWN'}
                      size="small"
                      color={
                        app.scoring_result?.risk_tier === 'SUPER_PRIME' || app.scoring_result?.risk_tier === 'PRIME'
                          ? 'success'
                          : app.scoring_result?.risk_tier === 'NEAR_PRIME'
                          ? 'warning'
                          : 'error'
                      }
                      sx={{ fontWeight: 700, fontSize: 11 }}
                    />
                  </TableCell>
                  <TableCell>
                    <Chip
                      label={app.status}
                      variant="outlined"
                      size="small"
                      color={
                        app.status === 'APPROVED'
                          ? 'success'
                          : app.status === 'IN_REVIEW'
                          ? 'warning'
                          : 'default'
                      }
                      sx={{ fontWeight: 600 }}
                    />
                  </TableCell>
                  <TableCell>
                    {app.status === 'IN_REVIEW' ? (
                      <Button
                        variant="contained"
                        color="warning"
                        size="small"
                        startIcon={<RateReviewIcon />}
                        onClick={() => handleOpenReview(app)}
                      >
                        Review Case
                      </Button>
                    ) : (
                      <Button
                        variant="outlined"
                        size="small"
                        onClick={() => handleOpenReview(app)}
                      >
                        View Details
                      </Button>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      {/* Underwriter Review Modal Dialog */}
      {selectedApp && (
        <Dialog open={dialogOpen} onClose={() => setDialogOpen(false)} maxWidth="md" fullWidth>
          <DialogTitle sx={{ fontWeight: 800 }}>
            Underwriter Adjudication — Case {selectedApp.id}
          </DialogTitle>
          <DialogContent dividers>
            <Box sx={{ mb: 3 }}>
              <Alert severity="info" sx={{ mb: 3 }}>
                <strong>Borrower Profile:</strong> {selectedApp.applicant?.first_name} {selectedApp.applicant?.last_name} | Monthly Income: {formatCentsToDollars(selectedApp.applicant?.monthly_income_cents || 0)}/mo | Stated Purpose: {selectedApp.loan_purpose}
              </Alert>

              <ShapWaterfall attributions={selectedApp.scoring_result?.feature_attributions || []} />
            </Box>

            {selectedApp.status === 'IN_REVIEW' && (
              <Box sx={{ mt: 3, p: 2, bgcolor: '#F8FAFC', borderRadius: 2, border: '1px solid #E2E8F0' }}>
                <Typography variant="subtitle1" sx={{ fontWeight: 700, mb: 2 }}>
                  Adjudication Decision Terms:
                </Typography>
                <Box sx={{ display: 'flex', gap: 2, mb: 2 }}>
                  <TextField
                    label="Adjusted Approved Limit ($ USD)"
                    type="number"
                    fullWidth
                    value={limitDollars}
                    onChange={(e) => setLimitDollars(Number(e.target.value))}
                  />
                  <TextField
                    label="Adjusted APR (%)"
                    type="number"
                    fullWidth
                    value={aprPercent}
                    onChange={(e) => setAprPercent(Number(e.target.value))}
                  />
                </Box>
                <TextField
                  label="Underwriter Adjudication Notes"
                  multiline
                  rows={2}
                  fullWidth
                  value={notes}
                  onChange={(e) => setNotes(e.target.value)}
                />
              </Box>
            )}
          </DialogContent>
          <DialogActions sx={{ p: 2.5 }}>
            <Button onClick={() => setDialogOpen(false)}>Cancel</Button>
            {selectedApp.status === 'IN_REVIEW' && (
              <>
                <Button
                  variant="contained"
                  color="error"
                  startIcon={<CancelIcon />}
                  onClick={() => handleExecuteDecision('DECLINE')}
                >
                  Decline Application
                </Button>
                <Button
                  variant="contained"
                  color="success"
                  startIcon={<CheckCircleIcon />}
                  onClick={() => handleExecuteDecision('APPROVE')}
                >
                  Approve (Fire Temporal Signal)
                </Button>
              </>
            )}
          </DialogActions>
        </Dialog>
      )}
    </Container>
  );
}
