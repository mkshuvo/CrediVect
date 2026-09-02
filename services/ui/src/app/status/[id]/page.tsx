"use client";

import React, { useEffect, useState } from 'react';
import { useParams } from 'next/navigation';
import { Container, Card, CardContent, Typography, Box, Chip, Button, Alert, CircularProgress, Divider } from '@mui/material';
import CheckCircleIcon from '@mui/icons-material/CheckCircle';
import HourglassEmptyIcon from '@mui/icons-material/HourglassEmpty';
import CancelIcon from '@mui/icons-material/Cancel';
import DownloadIcon from '@mui/icons-material/Download';
import { fetchApplication } from '@/lib/api';
import { formatCentsToDollars, formatBpsToPercent } from '@/lib/formatters';
import ShapWaterfall from '@/components/ShapWaterfall';

export default function ApplicationStatusPage() {
  const { id } = useParams();
  const [app, setApp] = useState<any>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!id) return;
    const load = async () => {
      try {
        const data = await fetchApplication(id as string);
        setApp(data);
      } catch (e) {
        console.error(e);
      } finally {
        setLoading(false);
      }
    };
    load();
    const interval = setInterval(load, 3000); // Live status polling
    return () => clearInterval(interval);
  }, [id]);

  if (loading) {
    return (
      <Container maxWidth="sm" sx={{ py: 10, textAlign: 'center' }}>
        <CircularProgress color="secondary" />
        <Typography variant="body1" sx={{ mt: 2 }}>Retrieving underwriting decision...</Typography>
      </Container>
    );
  }

  if (!app) {
    return (
      <Container maxWidth="sm" sx={{ py: 10 }}>
        <Alert severity="error">Application not found for ID: {id}</Alert>
      </Container>
    );
  }

  const isApproved = app.status === 'APPROVED';
  const isInReview = app.status === 'IN_REVIEW';
  const isDeclined = app.status === 'DECLINED';

  return (
    <Container maxWidth="md" sx={{ py: 6 }}>
      {/* 1. APPROVAL CARD */}
      {isApproved && (
        <Card sx={{ borderTop: '6px solid #10B981', mb: 4 }}>
          <CardContent sx={{ p: 4, textAlign: 'center' }}>
            <CheckCircleIcon sx={{ fontSize: 64, color: 'success.main', mb: 1 }} />
            <Typography variant="h4" sx={{ fontWeight: 800, color: 'success.dark', mb: 1 }}>
              Congratulations! Your Loan is Approved
            </Typography>
            <Typography variant="body1" color="text.secondary" sx={{ mb: 4 }}>
              Application ID: {app.id}
            </Typography>

            <Box sx={{ display: 'flex', justifyContent: 'center', gap: 4, mb: 4, p: 3, bgcolor: '#F0FDF4', borderRadius: 2 }}>
              <Box>
                <Typography variant="caption" color="text.secondary">APPROVED CREDIT LIMIT</Typography>
                <Typography variant="h5" sx={{ fontWeight: 800, color: 'success.dark' }}>
                  {formatCentsToDollars(app.decision?.approved_limit_cents || app.requested_amount_cents)}
                </Typography>
              </Box>
              <Divider orientation="vertical" flexItem />
              <Box>
                <Typography variant="caption" color="text.secondary">APPROVED APR</Typography>
                <Typography variant="h5" sx={{ fontWeight: 800, color: 'success.dark' }}>
                  {formatBpsToPercent(app.decision?.approved_apr_bps || app.scoring_result?.recommended_apr_bps || 950)}
                </Typography>
              </Box>
              <Divider orientation="vertical" flexItem />
              <Box>
                <Typography variant="caption" color="text.secondary">TERM</Typography>
                <Typography variant="h5" sx={{ fontWeight: 800, color: 'success.dark' }}>
                  {app.loan_term_months} Months
                </Typography>
              </Box>
            </Box>

            <Button variant="contained" color="success" size="large" sx={{ px: 5, py: 1.5 }}>
              Accept Offer & Sign Loan Agreement
            </Button>
          </CardContent>
        </Card>
      )}

      {/* 2. IN REVIEW CARD (Human-in-the-Loop) */}
      {isInReview && (
        <Card sx={{ borderTop: '6px solid #F59E0B', mb: 4 }}>
          <CardContent sx={{ p: 4, textAlign: 'center' }}>
            <HourglassEmptyIcon sx={{ fontSize: 64, color: 'warning.main', mb: 1 }} />
            <Typography variant="h4" sx={{ fontWeight: 800, color: 'warning.dark', mb: 1 }}>
              Application Under Credit Review
            </Typography>
            <Typography variant="body1" color="text.secondary" sx={{ mb: 3 }}>
              Your application is currently undergoing standard credit review by our risk underwriting desk.
            </Typography>
            <Alert severity="warning" sx={{ maxWidth: 600, mx: 'auto', textAlign: 'left' }}>
              <strong>Near-Prime Review Tier:</strong> Temporal.io has routed this case to an underwriter. The workflow will automatically finalize your decision within 24 to 48 hours.
            </Alert>
          </CardContent>
        </Card>
      )}

      {/* 3. DECLINED CARD (FCRA Adverse Action Notice) */}
      {isDeclined && (
        <Card sx={{ borderTop: '6px solid #EF4444', mb: 4 }}>
          <CardContent sx={{ p: 4 }}>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 2, mb: 2 }}>
              <CancelIcon sx={{ fontSize: 48, color: 'error.main' }} />
              <Box>
                <Typography variant="h5" sx={{ fontWeight: 800, color: 'error.dark' }}>
                  Notice of Adverse Action
                </Typography>
                <Typography variant="body2" color="text.secondary">
                  Formal Disclosure pursuant to the Fair Credit Reporting Act (15 U.S.C. § 1681)
                </Typography>
              </Box>
            </Box>

            <Divider sx={{ my: 2 }} />

            <Typography variant="body2" sx={{ mb: 2 }}>
              Thank you for your interest in credit with CrediVect. We regret to inform you that we are unable to approve your application at this time based on our quantitative credit scoring models.
            </Typography>

            <Typography variant="subtitle2" sx={{ fontWeight: 700, mb: 1 }}>
              Key Factors Contributing to this Decision (Ranked by Impact):
            </Typography>

            <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5, mb: 3 }}>
              {app.scoring_result?.adverse_action_reasons?.map((reason: any) => (
                <Card key={reason.reason_code} variant="outlined" sx={{ p: 2, bgcolor: '#FEF2F2' }}>
                  <Typography variant="body2" sx={{ fontWeight: 700, color: 'error.dark' }}>
                    Code {reason.reason_code}: {reason.description}
                  </Typography>
                </Card>
              ))}
            </Box>

            <Button variant="outlined" color="error" startIcon={<DownloadIcon />}>
              Download Official Adverse Action Notice (PDF)
            </Button>
          </CardContent>
        </Card>
      )}

      {/* SHAP Waterfall Explainability Breakdown */}
      {app.scoring_result?.feature_attributions && (
        <ShapWaterfall attributions={app.scoring_result.feature_attributions} />
      )}
    </Container>
  );
}
