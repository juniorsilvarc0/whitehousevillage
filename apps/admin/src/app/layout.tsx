import type { Metadata, Viewport } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "White House Village · Gestão",
  description: "Gestão de reservas, eventos e relacionamento da White House Village.",
};

export const viewport: Viewport = {
  themeColor: "#2c3329",
  viewportFit: "cover",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="pt-BR" suppressHydrationWarning>
      <body className="min-h-dvh antialiased">{children}</body>
    </html>
  );
}
