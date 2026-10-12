"use client";

import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { api, clearSession, getToken, getStoredUser, saveSession } from '@/lib/api';
import { AuthContext, SessionUser } from './AuthContext';

// Real session handling against core's /auth/login. Nothing here fabricates a
// signed-in identity: if there is no token in storage the app is unauthenticated
// and the server, not the client, decides what that means.
export default function LocalAuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<SessionUser | null>(null);
  const [token, setToken] = useState<string | null>(null);
  const [ready, setReady] = useState(false);

  useEffect(() => {
    const storedToken = getToken();
    const storedUser = getStoredUser();
    if (!storedToken || !storedUser) {
      setReady(true);
      return;
    }
    setToken(storedToken);
    setUser(storedUser);
    // Re-read the identity so a token whose user was deleted cannot keep the
    // stale profile on screen; a 401 clears the session.
    api
      .me()
      .then((fresh) => setUser(fresh))
      .catch(() => {
        clearSession();
        setUser(null);
        setToken(null);
      })
      .finally(() => setReady(true));
  }, []);

  const login = useCallback(async (email: string, password: string) => {
    const res = await api.login(email, password);
    saveSession(res.token, res.user);
    setToken(res.token);
    setUser(res.user);
    return res.user;
  }, []);

  const logout = useCallback(() => {
    clearSession();
    setUser(null);
    setToken(null);
  }, []);

  const value = useMemo(
    () => ({
      user,
      token,
      isAuthenticated: !!user && !!token,
      role: user?.role ?? null,
      ready,
      login,
      logout,
    }),
    [user, token, ready, login, logout],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
