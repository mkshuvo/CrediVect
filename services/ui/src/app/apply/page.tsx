"use client";

import React, { useMemo, useState } from 'react';
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
  CircularProgress,
} from '@mui/material';
import { api, ApiError } from '@/lib/api';
import type { NewApplication } from '@/lib/api';
import { formatCentsToDollars } from '@/lib/formatters';

const steps = ['Borrower Details', 'Loan Sizing & Terms', 'Review & FCRA Consent'];

const LOAN_PURPOSES: [string, string][] = [
  ['DEBT_CONSOLIDATION', 'Debt Consolidation'],
  ['HOME_IMPROVEMENT', 'Home Improvement'],
  ['AUTO', 'Vehicle Purchase'],
  ['EDUCATION', 'Education'],
  ['MEDICAL', 'Medical Expenses'],
  ['SMALL_BUSINESS', 'Small Business'],
  ['LARGE_PURCHASE', 'Major Purchase'],
];

// Must match api.normaliseEmployment in core; anything else silently becomes
// EMPLOYED server-side, so the wizard only offers values the server understands.
const EMPLOYMENT_STATUSES: [string, string][] = [
  ['EMPLOYED', 'Employed'],
  ['SELF_EMPLOYED', 'Self-Employed'],
  ['RETIRED', 'Retired'],
  ['STUDENT', 'Student'],
  ['UNEMPLOYED', 'Unemployed'],
];

const LOAN_TERMS = [12, 24, 36, 48, 60, 72, 84];

// Core rejects requests without an Idempotency-Key retry, and replays the stored
// response for a key it has already accepted, so one key per mounted wizard makes
// a double click or a browser retry a single origination.
function newIdempotencyKey(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  return `idemp-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}

export default function LoanOriginationWizard() {
  const router = useRouter();
  const idempotencyKey = useMemo(newIdempotencyKey, []);
  const [activeStep, setActiveStep] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});

  // Money and income are held in dollars in the form and converted to integer
  // cents on submit; no float ever reaches the API.
  const [firstName, setFirstName] = useState('');
  const [lastName, setLastName] = useState('');
  const [email, setEmail] = useState('');
  const [dateOfBirth, setDateOfBirth] = useState('');
  const [ssn, setSsn] = useState('');
  const [monthlyIncomeDollars, setMonthlyIncomeDollars] = useState(6000);
  const [employmentStatus, setEmploymentStatus] = useState('EMPLOYED');
  const [requestedAmountDollars, setRequestedAmountDollars] = useState(12000);
  const [loanTermMonths, setLoanTermMonths] = useState(36);
  const [loanPurpose, setLoanPurpose] = useState('DEBT_CONSOLIDATION');
  const [fcraConsent, setFcraConsent] = useState(false);

  const purposeLabel = useMemo(
    () => LOAN_PURPOSES.find(([value]) => value === loanPurpose)?.[1] ?? loanPurpose,
    [loanPurpose],
  );

  const resetErrors = () => {
    setError(null);
    setFieldErrors({});
  };

  const handleNext = () => {
    resetErrors();
    if (activeStep === 0) {
      const missing: Record<string, string> = {};
      if (firstName.trim().length < 2) missing.first_name = 'Enter a first name of at least 2 characters.';
      if (lastName.trim().length < 2) missing.last_name = 'Enter a last name of at least 2 characters.';
      if (!/^[^@\s]+@[^@\s]+\.[A-Za-z]{2,}$/.test(email.trim())) missing.email = 'Enter a valid email address.';
      if (!dateOfBirth) missing.date_of_birth = 'Date of birth is required.';
      if (ssn.replace(/\D/g, '').length !== 9) missing.ssn = 'Enter the 9-digit SSN.';
      if (monthlyIncomeDollars < 500 || monthlyIncomeDollars > 500000) {
        missing.monthly_income_cents = 'Monthly income must be between $500 and $500,000.';
      }
      if (Object.keys(missing).length > 0) {
        setFieldErrors(missing);
        return;
      }
    }
    setActiveStep((prev) => prev + 1);
  };

  const handleBack = () => {
    resetErrors();
    setActiveStep((prev) => prev - 1);
  };

  const handleSubmit = async () => {
    if (!fcraConsent) {
      setError('FCRA permissible-purpose consent is required before a credit file can be pulled.');
      return;
    }
    setLoading(true);
    resetErrors();

    const payload: NewApplication = {
      first_name: firstName.trim(),
      last_name: lastName.trim(),
      email: email.trim(),
      date_of_birth: dateOfBirth,
      ssn: ssn.replace(/\D/g, ''),
      monthly_income_cents: Math.round(monthlyIncomeDollars * 100),
      requested_amount_cents: Math.round(requestedAmountDollars * 100),
      loan_term_months: loanTermMonths,
      loan_purpose: loanPurpose,
      employment_status: employmentStatus,
      consent_fcra: true,
    };

    try {
      const result = await api.submitApplication(payload, idempotencyKey);
      // The status token is shown exactly once here; a URL fragment keeps it out
      // of server access logs and it survives a reload of the status page.
      router.push(`/status/${result.application.id}#t=${encodeURIComponent(result.status_token)}`);
    } catch (err) {
      if (err instanceof ApiError) {
        setError(err.message);
        setFieldErrors(err.fieldErrors);
      } else {
        setError('Unexpected error submitting the application.');
      }
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
            Instant credit evaluation backed by a live bureau file pull and dual-engine risk scoring
          </Typography>

          <Stepper activeStep={activeStep} alternativeLabel sx={{ mb: 5 }}>
            {steps.map((label) => (
              <Step key={label}>
                <StepLabel>{label}</StepLabel>
              </Step>
            ))}
          </Stepper>

          {error && (
            <Alert severity="error" sx={{ mb: 3 }} onClose={resetErrors}>
              {error}
            </Alert>
          )}

          {/* STEP 1: Personal Information */}
          {activeStep === 0 && (
            <Box sx={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
              <Box sx={{ display: 'flex', gap: 2 }}>
                <TextField
                  label="First Name"
                  fullWidth
                  value={firstName}
                  onChange={(e) => setFirstName(e.target.value)}
                  error={!!fieldErrors.first_name}
                  helperText={fieldErrors.first_name}
                  required
                />
                <TextField
                  label="Last Name"
                  fullWidth
                  value={lastName}
                  onChange={(e) => setLastName(e.target.value)}
                  error={!!fieldErrors.last_name}
                  helperText={fieldErrors.last_name}
                  required
                />
              </Box>
              <TextField
                label="Email Address"
                type="email"
                fullWidth
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                error={!!fieldErrors.email}
                helperText={fieldErrors.email}
                required
              />
              <TextField
                label="Date of Birth"
                type="date"
                fullWidth
                value={dateOfBirth}
                onChange={(e) => setDateOfBirth(e.target.value)}
                InputLabelProps={{ shrink: true }}
                error={!!fieldErrors.date_of_birth}
                helperText={fieldErrors.date_of_birth || 'Borrower must be at least 18 years old.'}
                required
              />
              <TextField
                label="Social Security Number"
                fullWidth
                value={ssn}
                onChange={(e) => setSsn(e.target.value.replace(/[^0-9]/g, '').slice(0, 9))}
                error={!!fieldErrors.ssn}
                helperText={
                  fieldErrors.ssn ||
                  'Transmitted once, then stored as an HMAC blind index plus an AES-256-GCM sealed record. The plaintext is never persisted.'
                }
                inputMode="numeric"
                autoComplete="off"
                required
              />
              <TextField
                label="Gross Monthly Income ($ USD)"
                type="number"
                fullWidth
                value={monthlyIncomeDollars}
                onChange={(e) => setMonthlyIncomeDollars(Number(e.target.value))}
                error={!!fieldErrors.monthly_income_cents}
                helperText={
                  fieldErrors.monthly_income_cents ||
                  'Sent as integer minor units ($5,000.00 becomes 500000 cents); the platform does not carry floats.'
                }
                required
              />
              <FormControl fullWidth>
                <InputLabel>Employment Status</InputLabel>
                <Select
                  value={employmentStatus}
                  label="Employment Status"
                  onChange={(e) => setEmploymentStatus(e.target.value)}
                >
                  {EMPLOYMENT_STATUSES.map(([value, label]) => (
                    <MenuItem key={value} value={value}>
                      {label}
                    </MenuItem>
                  ))}
                </Select>
              </FormControl>
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
                  marks
                  valueLabelDisplay="auto"
                  onChange={(_, val) => setRequestedAmountDollars(val as number)}
                  color="secondary"
                />
                <Box sx={{ display: 'flex', justifyContent: 'space-between' }}>
                  <Typography variant="caption" color="text.secondary">
                    $1,000
                  </Typography>
                  <Typography variant="caption" color="text.secondary">
                    $50,000
                  </Typography>
                </Box>
              </Box>

              <FormControl fullWidth>
                <InputLabel>Loan Term</InputLabel>
                <Select
                  value={loanTermMonths}
                  label="Loan Term"
                  onChange={(e) => setLoanTermMonths(Number(e.target.value))}
                >
                  {LOAN_TERMS.map((months) => (
                    <MenuItem key={months} value={months}>
                      {months} Months ({(months / 12).toLocaleString(undefined, { maximumFractionDigits: 1 })} Years)
                    </MenuItem>
                  ))}
                </Select>
              </FormControl>

              <FormControl fullWidth>
                <InputLabel>Loan Purpose</InputLabel>
                <Select
                  value={loanPurpose}
                  label="Loan Purpose"
                  onChange={(e) => setLoanPurpose(e.target.value)}
                >
                  {LOAN_PURPOSES.map(([value, label]) => (
                    <MenuItem key={value} value={value}>
                      {label}
                    </MenuItem>
                  ))}
                </Select>
              </FormControl>

              {fieldErrors.loan_term_months && <Alert severity="error">{fieldErrors.loan_term_months}</Alert>}
              {fieldErrors.loan_purpose && <Alert severity="error">{fieldErrors.loan_purpose}</Alert>}
              {fieldErrors.requested_amount_cents && <Alert severity="error">{fieldErrors.requested_amount_cents}</Alert>}
            </Box>
          )}

          {/* STEP 3: Review & Consent */}
          {activeStep === 2 && (
            <Box sx={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
              <Alert severity="info">
                <Typography variant="subtitle2" sx={{ fontWeight: 700, mb: 1 }}>
                  Application Summary
                </Typography>
                Borrower: {firstName} {lastName} ({email}), {dateOfBirth}<br />
                Monthly income: {formatCentsToDollars(Math.round(monthlyIncomeDollars * 100))} ({employmentStatus})<br />
                Loan: {formatCentsToDollars(Math.round(requestedAmountDollars * 100))} over {loanTermMonths} months
                {' '}for {purposeLabel}
              </Alert>

              <Card variant="outlined" sx={{ p: 2, bgcolor: '#F8FAFC' }}>
                <Typography variant="subtitle2" sx={{ fontWeight: 700, mb: 1 }}>
                  Fair Credit Reporting Act (FCRA) Disclosure &amp; Consent
                </Typography>
                <Typography variant="body2" color="text.secondary" sx={{ fontSize: 13 }}>
                  By checking the box below you authorize CrediVect and its lending partners to obtain your consumer
                  credit report from a consumer reporting agency for the sole purpose of evaluating this application. If
                  the application is declined you will receive an Adverse Action Notice naming the agency whose report
                  was used, the score, and the principal reasons for the decision.
                </Typography>
              </Card>

              <FormControlLabel
                control={
                  <Checkbox
                    checked={fcraConsent}
                    onChange={(e) => {
                      setFcraConsent(e.target.checked);
                      setFieldErrors((prev) => ({ ...prev, consent_fcra: '' }));
                    }}
                    color="secondary"
                  />
                }
                label="I consent to a credit file pull under FCRA permissible purpose."
              />
              {fieldErrors.consent_fcra && <Alert severity="error">{fieldErrors.consent_fcra}</Alert>}
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
