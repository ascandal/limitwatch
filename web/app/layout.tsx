import type { Metadata } from 'next';
import './globals.css';

export const metadata: Metadata = {
  title: 'LimitWatch | Operator console',
  description:
    'Inspect alarms, acknowledge conditions, and follow their history.',
};
export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
