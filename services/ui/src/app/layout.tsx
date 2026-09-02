import type { Metadata } from 'next';
import ThemeRegistry from '@/theme/ThemeRegistry';
import LocalAuthProvider from '@/lib/auth/LocalAuthProvider';
import Navbar from '@/components/Navbar';

export const metadata: Metadata = {
  title: 'CrediVect — Enterprise Credit Risk Scoring & Underwriting',
  description: 'Fault-tolerant credit risk scoring engine with durable orchestration and TreeSHAP explainability.',
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en">
      <body>
        <ThemeRegistry>
          <LocalAuthProvider>
            <Navbar />
            <main>{children}</main>
          </LocalAuthProvider>
        </ThemeRegistry>
      </body>
    </html>
  );
}
