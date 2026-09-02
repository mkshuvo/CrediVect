"use client";

import React, { useState, useEffect } from 'react';
import { AuthContext, User } from './AuthContext';

export default function LocalAuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [token, setToken] = useState<string | null>(null);

  useEffect(() => {
    // Check saved session
    const saved = localStorage.getItem('credivect_user');
    const savedToken = localStorage.getItem('credivect_token');
    if (saved && savedToken) {
      try {
        setUser(JSON.parse(saved));
        setToken(savedToken);
      } catch (e) {
        localStorage.clear();
      }
    } else {
      // Default to Underwriter session for immediate dashboard testing
      const defaultUnderwriter: User = {
        id: 'usr-underwriter-sarah',
        email: 'underwriter@credivect.com',
        firstName: 'Sarah',
        lastName: 'Jenkins',
        role: 'UNDERWRITER',
      };
      setUser(defaultUnderwriter);
      setToken('demo-underwriter-session-token');
    }
  }, []);

  const login = async (email: string) => {
    let role: 'APPLICANT' | 'UNDERWRITER' | 'COMPLIANCE_ADMIN' = 'APPLICANT';
    let firstName = 'Alex';
    let lastName = 'Chen';

    if (email.includes('underwriter')) {
      role = 'UNDERWRITER';
      firstName = 'Sarah';
      lastName = 'Jenkins';
    } else if (email.includes('admin')) {
      role = 'COMPLIANCE_ADMIN';
      firstName = 'David';
      lastName = 'Vogel';
    }

    const newUser: User = {
      id: 'usr-' + Date.now(),
      email,
      firstName,
      lastName,
      role,
    };
    const newToken = 'session-token-' + Date.now();
    setUser(newUser);
    setToken(newToken);
    localStorage.setItem('credivect_user', JSON.stringify(newUser));
    localStorage.setItem('credivect_token', newToken);
  };

  const logout = () => {
    setUser(null);
    setToken(null);
    localStorage.removeItem('credivect_user');
    localStorage.removeItem('credivect_token');
  };

  return (
    <AuthContext.Provider
      value={{
        user,
        token,
        isAuthenticated: !!user,
        role: user?.role || 'APPLICANT',
        login,
        logout,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
}
