"use client";

import React from 'react';
import Link from 'next/link';
import { Container, Typography, Box, Button, Grid, Card, CardContent, Chip } from '@mui/material';
import SpeedIcon from '@mui/icons-material/Speed';
import RuleIcon from '@mui/icons-material/Rule';
import VerifiedUserIcon from '@mui/icons-material/VerifiedUser';
import AccountTreeIcon from '@mui/icons-material/AccountTree';

export default function HomePage() {
  return (
    <Box sx={{ py: 8 }}>
      <Container maxWidth="lg">
        <Box sx={{ textAlign: 'center', mb: 8 }}>
          <Chip label="Zero-Float Financial Architecture" color="secondary" size="small" sx={{ mb: 2, fontWeight: 600 }} />
          <Typography variant="h3" sx={{ fontWeight: 800, mb: 2, color: 'primary.main' }}>
            Institutional Credit Risk Scoring & Underwriting Engine
          </Typography>
          <Typography variant="h6" color="text.secondary" sx={{ maxWidth: 800, mx: 'auto', mb: 4, fontWeight: 400 }}>
            Automate borrower risk evaluation in under 15ms using monotonic LightGBM scorecards, durable Temporal.io sagas, and legally mandated TreeSHAP explainability.
          </Typography>
          <Box sx={{ display: 'flex', justifyContent: 'center', gap: 2 }}>
            <Button component={Link} href="/apply" variant="contained" size="large" color="secondary" sx={{ px: 4, py: 1.5 }}>
              Originate Loan Application
            </Button>
            <Button component={Link} href="/dashboard" variant="outlined" size="large" color="primary" sx={{ px: 4, py: 1.5 }}>
              Open Underwriter Queue
            </Button>
          </Box>
        </Box>

        {/* Feature Highlights Grid */}
        <Grid container spacing={3}>
          <Grid item xs={12} md={3}>
            <Card sx={{ height: '100%' }}>
              <CardContent>
                <SpeedIcon sx={{ fontSize: 40, color: 'secondary.main', mb: 1 }} />
                <Typography variant="h6" sx={{ fontWeight: 700, mb: 1 }}>
                  Sub-15ms Latency SLA
                </Typography>
                <Typography variant="body2" color="text.secondary">
                  Dual-engine quantitative scoring: Weight of Evidence (WoE) 300-850 scorecard plus calibrated LightGBM
                  default probability (PD).
                </Typography>
              </CardContent>
            </Card>
          </Grid>
          <Grid item xs={12} md={3}>
            <Card sx={{ height: '100%' }}>
              <CardContent>
                <AccountTreeIcon sx={{ fontSize: 40, color: 'warning.main', mb: 1 }} />
                <Typography variant="h6" sx={{ fontWeight: 700, mb: 1 }}>
                  Temporal Durable Sagas
                </Typography>
                <Typography variant="body2" color="text.secondary">
                  Zero dropped applications. Resilient retries on 3rd-party credit bureau timeouts and 72-hour Human-in-the-Loop review pauses.
                </Typography>
              </CardContent>
            </Card>
          </Grid>
          <Grid item xs={12} md={3}>
            <Card sx={{ height: '100%' }}>
              <CardContent>
                <RuleIcon sx={{ fontSize: 40, color: 'success.main', mb: 1 }} />
                <Typography variant="h6" sx={{ fontWeight: 700, mb: 1 }}>
                  FCRA TreeSHAP Explainability
                </Typography>
                <Typography variant="body2" color="text.secondary">
                  Legally compliant Adverse Action Notices naming the agency and the ranked reasons behind the decision, up to the four principal ones (AA01-AA05).
                </Typography>
              </CardContent>
            </Card>
          </Grid>
          <Grid item xs={12} md={3}>
            <Card sx={{ height: '100%' }}>
              <CardContent>
                <VerifiedUserIcon sx={{ fontSize: 40, color: 'primary.main', mb: 1 }} />
                <Typography variant="h6" sx={{ fontWeight: 700, mb: 1 }}>
                  Cryptographic Merkle Ledger
                </Typography>
                <Typography variant="body2" color="text.secondary">
                  Uber LedgerStore pattern: Append-only hash chaining providing tamper-evident auditability for federal banking exams.
                </Typography>
              </CardContent>
            </Card>
          </Grid>
        </Grid>
      </Container>
    </Box>
  );
}
