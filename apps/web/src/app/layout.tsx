import type { Metadata } from "next";
import { Geist, Geist_Mono } from "next/font/google";
import { PlayerBar } from "@/components/player-bar";
import "./globals.css";

const geistSans = Geist({
  variable: "--font-geist-sans",
  subsets: ["latin"],
});

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  title: "Goonj — every voice deserves an echo",
  description: "Audio-only platform: on-demand shows and live audio rooms.",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html
      lang="en"
      className={`${geistSans.variable} ${geistMono.variable} h-full antialiased`}
    >
      <body className="min-h-full flex flex-col pb-24">
        {children}
        <PlayerBar />
      </body>
    </html>
  );
}
