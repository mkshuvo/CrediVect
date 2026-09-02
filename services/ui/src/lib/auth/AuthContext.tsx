"use client";

import React, { createContext, useContext, useState, useEffect } from 'react';

export interface User {
  id: string;
  email: string;
  firstName: string;
  lastName: string;
  role: 'APPLICANT' | 'UNDERWRITER' | 'COMPLIANCE_ADMIN';
}

export interface AuthContextType {
  user: User | null;
  token: string | null;
  isAuthenticated: boolean;
  role: 'APPLICANT' | 'UNDERWRITER' | 'COMPLIANCE_ADMIN';
  login: (email: string) => Promise<void>;
  logout: () => void;
}

export const AuthContext = createContext<AuthContextType>({
  user: null,
  token: null,
  isAuthenticated: false,
  role: 'APPLICANT',
  login: async () => {},
  logout: () => {},
});

export const useAuth = () => useContext(AuthContext);
