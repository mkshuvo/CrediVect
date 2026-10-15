"use client";

import React from 'react';
import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { AppBar, Toolbar, Typography, Button, Box, Chip } from '@mui/material';
import ShieldIcon from '@mui/icons-material/Security';
import DashboardIcon from '@mui/icons-material/Dashboard';
import PostAddIcon from '@mui/icons-material/PostAdd';
import HistoryEduIcon from '@mui/icons-material/HistoryEdu';
import AssessmentIcon from '@mui/icons-material/Assessment';
import { useAuth } from '@/lib/auth/AuthContext';

// Labels collapse to icons below `sm`, and the decorative chips drop out below
// `md`. Without this the toolbar needs ~910px and the whole page gains a
// horizontal scrollbar on a tablet or phone. Every control keeps an aria-label
// so the icon-only form still names its destination.
const LABEL = { display: { xs: 'none', sm: 'inline' } };

export default function Navbar() {
  const { user, role, isAuthenticated, ready, logout } = useAuth();
  const pathname = usePathname();

  const queueVisible = role === 'UNDERWRITER' || role === 'ADMIN';
  const myCasesVisible = role === 'BORROWER';

  const weight = (href: string) => (pathname === href ? 700 : 400);

  return (
    <AppBar position="sticky" sx={{ bgcolor: 'primary.main', borderBottom: '1px solid rgba(255,255,255,0.1)' }}>
      <Toolbar sx={{ justifyContent: 'space-between', gap: 1 }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, minWidth: 0 }}>
          <ShieldIcon sx={{ color: 'secondary.light', fontSize: 28, flexShrink: 0 }} />
          <Link href="/" style={{ textDecoration: 'none', color: 'inherit' }}>
            <Typography variant="h6" noWrap sx={{ fontWeight: 800, letterSpacing: '0.05em' }}>
              Credi<span style={{ color: '#38BDF8' }}>Vect</span>
            </Typography>
          </Link>
          <Chip
            label="v1.0 Institutional"
            size="small"
            sx={{
              bgcolor: 'rgba(255,255,255,0.15)',
              color: 'white',
              ml: 1,
              fontSize: 11,
              display: { xs: 'none', md: 'inline-flex' },
            }}
          />
        </Box>

        <Box sx={{ display: 'flex', alignItems: 'center', gap: { xs: 0.5, sm: 2 } }}>
          <Button
            component={Link}
            href="/apply"
            aria-label="Apply for a loan"
            startIcon={<PostAddIcon />}
            sx={{ color: 'white', fontWeight: weight('/apply') }}
          >
            <Box component="span" sx={LABEL}>Apply for Loan</Box>
          </Button>
          {queueVisible && (
            <Button
              component={Link}
              href="/dashboard"
              aria-label="Underwriter queue"
              startIcon={<DashboardIcon />}
              sx={{ color: 'white', fontWeight: weight('/dashboard') }}
            >
              <Box component="span" sx={LABEL}>Underwriter Queue</Box>
            </Button>
          )}
          {queueVisible && (
            <Button
              component={Link}
              href="/dashboard/governance"
              aria-label="Governance and audit"
              startIcon={<HistoryEduIcon />}
              sx={{ color: 'white', fontWeight: weight('/dashboard/governance') }}
            >
              <Box component="span" sx={LABEL}>Governance &amp; Audit</Box>
            </Button>
          )}
          {myCasesVisible && (
            <Button
              component={Link}
              href="/my-applications"
              aria-label="My applications"
              startIcon={<AssessmentIcon />}
              sx={{ color: 'white', fontWeight: weight('/my-applications') }}
            >
              <Box component="span" sx={LABEL}>My Applications</Box>
            </Button>
          )}

          {ready && isAuthenticated ? (
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
              <Chip
                label={`${user?.name || user?.email} (${role})`}
                color={role === 'UNDERWRITER' ? 'warning' : 'secondary'}
                size="small"
                sx={{ fontWeight: 600, display: { xs: 'none', md: 'inline-flex' } }}
              />
              <Button
                onClick={logout}
                variant="outlined"
                size="small"
                aria-label="Sign out"
                sx={{ color: 'white', borderColor: 'rgba(255,255,255,0.3)' }}
              >
                Logout
              </Button>
            </Box>
          ) : (
            ready && (
              <Button component={Link} href="/login" variant="contained" color="secondary" size="small">
                Sign In
              </Button>
            )
          )}
        </Box>
      </Toolbar>
    </AppBar>
  );
}
