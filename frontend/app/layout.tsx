import type { Metadata } from "next";
import { Geist_Mono, Inter, Open_Sans, Source_Sans_3 } from "next/font/google";
import "./globals.css";
import { QueryProviders } from "../providers/query-providers";
import { RuntimeConfigScript } from "../components/runtime-config-script";

// The three families the JIVE designs use: Inter for headings and chrome,
// Source Sans 3 for ticket-card copy, Open Sans for prices.
const inter = Inter({
  variable: "--font-sans",
  subsets: ["latin"],
});

const sourceSans = Source_Sans_3({
  variable: "--font-ticket",
  subsets: ["latin"],
});

const openSans = Open_Sans({
  variable: "--font-price",
  subsets: ["latin"],
});

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  title: "Event Ticketing",
  description: "Browse events, buy tickets as a guest, and receive them by email.",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html
      lang="en"
      className={`${inter.variable} ${sourceSans.variable} ${openSans.variable} ${geistMono.variable} h-full antialiased`}
    >
      <head>
        {/* Must precede the application bundle: the API base URL it carries is
            read on every fetch the browser makes. */}
        <RuntimeConfigScript />
      </head>
      <body className="flex min-h-full flex-col bg-background text-foreground">
        <QueryProviders>
          {children}
        </QueryProviders>
      </body>
    </html>
  );
}
