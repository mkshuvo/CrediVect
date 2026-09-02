"use client";

import React, { useState } from 'react';
import { useRouter } from 'next/navigation';
import { Container, Card, CardContent, Typography, TextField, Button, Box, Alert } from '@mui/material';
import { useAuth } from '@/lib/auth/AuthContext';

export default function LoginPage() {
  const router = useRouter();
  const { login } = useAuth();
  const [email, setEmail] = useState('underwriter@credivect.com');
  const [password, setPassword] = useState('MasterPass123!');

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    await login(email);
    if (email.includes('underwriter')) {
      router.push('/dashboard');
    } else {
      router.push('/apply');
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
            Pluggable Auth: Select demo role or enter credentials
          </Typography>

          <Alert severity="info" sx={{ mb: 3, fontSize: 12 }}>
            <strong>Demo Accounts:</strong><br />
            • Underwriter: <code>underwriter@credivect.com</code><br />
            • Compliance: <code>admin@credivect.com</code><br />
            • Borrower: <code>alex@example.com</code>
          </Alert>

          <Box component="form" onSubmit={handleSubmit} sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
            <TextField
              label="Email Address"
              fullWidth
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required
            />
            <TextField
              label="Password"
              type="password"
              fullWidth
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
            />
            <Button type="submit" variant="contained" color="secondary" size="large" sx={{ mt: 1 }}>
              Authenticate
            </Button>
          </Box>
        </CardContent>
      </Card>
    </Container>
  );
}
