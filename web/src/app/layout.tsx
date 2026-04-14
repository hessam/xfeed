import "./globals.css";
import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "TheFeed",
  description: "DNS-based feed reference client"
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
