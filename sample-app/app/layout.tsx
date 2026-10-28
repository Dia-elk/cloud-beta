import "./globals.css";
import type { ReactNode } from "react";
export const metadata = { title: "Sample App — Client Tracker" };
export default function Root({ children }: { children: ReactNode }) {
  return (<html lang="en"><body>{children}</body></html>);
}
