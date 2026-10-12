"use client";

import React from 'react';
import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { AppBar, Toolbar, Typography, Button, Box, Chip } from '@mui/material';
import ShieldIcon from '@mui/icons-material/Security';
import DashboardIcon from '@mui/icons-material/Dashboard';
import PostAddIcon from '@mui/icons-material/PostAdd';
import HistoryEduIcon from '@mui/icons-material/HistoryEdu';
import { useAuth } from '@/lib/auth/AuthContext';

export default function Navbar() {
  const { user, role, isAuthenticated, ready, logout } = useAuth();
  const pathname = usePathname();

  const queueVisible = role === 'UNDERWRITER' || role === 'ADMIN';

  return (
    <AppBar position="sticky" sx={{ bgcolor: 'primary.main', borderBottom: '1px solid rgba(255,255,255,0.1)' }}>
      <Toolbar sx={{ justifyContent: 'space-between' }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          <ShieldIcon sx={{ color: 'secondary.light', fontSize: 28 }} />
          <Link href="/" style={{ textDecoration: 'none', color: 'inherit' }}>
            <Typography variant="h6" sx={{ fontWeight: 800, letterSpacing: '0.05em' }}>
              Credi<span style={{ color: '#38BDF8' }}>Vect</span>
            </Typography>
          </Link>
          <Chip
            label="v1.0 Institutional"
            size="small"
            sx={{ bgcolor: 'rgba(255,255,255,0.15)', color: 'white', ml: 1, fontSize: 11 }}
          />
        </Box>

        <Box sx={{ display: 'flex', alignItems: 'center', gap: 2 }}>
          <Button
            component={Link}
            href="/apply"
            startIcon={<PostAddIcon />}
            sx={{ color: 'white', fontWeight: pathname === '/apply' ? 700 : 400 }}
          >
            Apply for Loan
          </Button>
          {queueVisible && (
            <Button
              component={Link}
              href="/dashboard"
              startIcon={<DashboardIcon />}
              sx={{ color: 'white', fontWeight: pathname === '/dashboard' ? 700 : 400 }}
            >
              Underwriter Queue
            </Button>
          )}
          {queueVisible && (
            <Button
              component={Link}
              href="/dashboard/governance"
              startIcon={<HistoryEduIcon />}
              sx={{ color: 'white', fontWeight: pathname === '/dashboard/governance' ? 700 : 400 }}
            >
              Governance &amp; Audit
            </Button>
          )}

          {ready && isAuthenticated ? (
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, ml: 2 }}>
              <Chip
                label={`${user?.name || user?.email} (${role})`}
                color={role === 'UNDERWRITER' ? 'warning' : 'secondary'}
                size="small"
                sx={{ fontWeight: 600 }}
              />
              <Button
                onClick={logout}
                variant="outlined"
                size="small"
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
