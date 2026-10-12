"use client";

import React, { useState } from 'react';
import { useRouter } from 'next/navigation';
import {
  Container,
  Card,
  CardContent,
  Typography,
  TextField,
  Button,
  Box,
  Alert,
  CircularProgress,
  Divider,
} from '@mui/material';
import { useAuth } from '@/lib/auth/AuthContext';
import { ApiError } from '@/lib/api';

// The seeded accounts are real rows in the users table with bcrypt hashes, so
// offering them as one-click fills is a shortcut into the demo book, not a
// simulated login.
const DEMO_ACCOUNTS = [
  { label: 'Underwriter', email: 'sarah.jenkins@credivect.example', password: 'credivect-underwriter' },
  { label: 'Risk Admin', email: 'risk.admin@credivect.example', password: 'credivect-admin' },
  { label: 'Borrower', email: 'alex.chen@example.com', password: 'credivect-borrower' },
];

export default function LoginPage() {
  const router = useRouter();
  const { login } = useAuth();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const user = await login(email, password);
      // Core owns the role; the destination follows from it rather than from the
      // shape of the email address.
      router.push(user.role === 'BORROWER' ? '/apply' : '/dashboard');
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Sign-in failed. Is the core service running?');
      setBusy(false);
    }
  };

  return (
    <Container maxWidth="xs" sx={{ py: 10 }}>
      <Card>
        <CardContent sx={{ p: 4 }}>
          <Typography variant="h5" sx={{ fontWeight: 700, mb: 1, textAlign: 'center' }}>
            Sign In to CrediVect
          </Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 3, textAlign: 'center' }}>
            HS256 session tokens issued by the core service, verified against bcrypt password hashes
          </Typography>

          {error && (
            <Alert severity="error" sx={{ mb: 2 }} onClose={() => setError(null)}>
              {error}
            </Alert>
          )}

          <Box component="form" onSubmit={handleSubmit} sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
            <TextField
              label="Email Address"
              type="email"
              fullWidth
              autoComplete="username"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required
            />
            <TextField
              label="Password"
              type="password"
              fullWidth
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
            />
            <Button type="submit" variant="contained" color="secondary" size="large" sx={{ mt: 1 }} disabled={busy}>
              {busy ? <CircularProgress size={22} color="inherit" /> : 'Authenticate'}
            </Button>
          </Box>

          <Divider sx={{ my: 3 }}>
            <Typography variant="caption" color="text.secondary">
              SEEDED ACCOUNTS
            </Typography>
          </Divider>

          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
            {DEMO_ACCOUNTS.map((account) => (
              <Button
                key={account.email}
                variant="outlined"
                size="small"
                onClick={() => {
                  setEmail(account.email);
                  setPassword(account.password);
                }}
              >
                {account.label}: {account.email}
              </Button>
            ))}
          </Box>
        </CardContent>
      </Card>
    </Container>
  );
}
