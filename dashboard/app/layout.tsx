import "./globals.css";
import type { ReactNode } from "react";
export const metadata = { title: "Cloud Beta — Console" };
export default function Root({ children }: { children: ReactNode }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
