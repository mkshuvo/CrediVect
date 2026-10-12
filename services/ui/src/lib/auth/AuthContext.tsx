"use client";

import React, { createContext, useContext } from 'react';
import type { Role, SessionUser } from '@/lib/api';

export type { Role, SessionUser };

export interface AuthContextType {
  user: SessionUser | null;
  token: string | null;
  isAuthenticated: boolean;
  role: Role | null;
  ready: boolean;
  login: (email: string, password: string) => Promise<SessionUser>;
  logout: () => void;
}

export const AuthContext = createContext<AuthContextType>({
  user: null,
  token: null,
  isAuthenticated: false,
  role: null,
  ready: false,
  login: async () => ({ id: '', email: '', name: '', role: 'BORROWER' }),
  logout: () => {},
});

export const useAuth = () => useContext(AuthContext);
