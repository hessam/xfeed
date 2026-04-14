"use client";

import { useMemo, useState } from "react";
import { readEncryptedFeedCache, writeEncryptedFeedCache } from "../../lib/cache";
import { probeResolvers, fetchMultipartCiphertext, type ResolverName } from "../lib/dns-fetcher";
import { decryptFeedCiphertext } from "../lib/feed-crypto";
import { exchangeAndUnwrapToken } from "../../lib/token-flow";
import { TelemetryOverlay } from "../components/telemetry-overlay";

type FeedItem = { source: string; text: string; time: string };

const AUTH_BASE = (process.env.NEXT_PUBLIC_AUTH_BASE_URL ?? "https://auth.example.com").trim();
const DOMAIN = (process.env.NEXT_PUBLIC_DEFAULT_DOMAIN ?? "t.example.com").trim();

function hasRTL(text: string): boolean {
  return /[\u0590-\u08FF]/.test(text);
}

export default function HomePage() {
  const [token, setToken] = useState("");
  const [sessionKey, setSessionKey] = useState<string | null>(null);
  const [feed, setFeed] = useState<FeedItem[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [telemetry, setTelemetry] = useState<{
    t1?: number;
    t2?: number;
    t3?: number;
    t4?: number;
    resolver?: string;
    parts?: number;
    dnsQueries?: number;
  }>({});

  const cached = useMemo(() => readEncryptedFeedCache(), []);

  async function handleTokenSubmit() {
    setLoading(true);
    setError(null);
    try {
      // Warm-up resolvers in parallel with auth exchange.
      const probeTask = probeResolvers(DOMAIN);
      const auth = await exchangeAndUnwrapToken(AUTH_BASE, token.trim());
      setSessionKey(auth.sessionKey);

      const probe = await probeTask;
      const fastest: ResolverName =
        probe.filter((p) => p.ok).sort((a, b) => a.latencyMs - b.latencyMs)[0]?.resolver ?? "cloudflare";

      const t4Start = performance.now();
      const dns = await fetchMultipartCiphertext(DOMAIN, fastest);
      const jsonText = await decryptFeedCiphertext(dns.ciphertextB64, auth.sessionKey);
      const parsed = JSON.parse(jsonText) as FeedItem[];
      setFeed(parsed);
      writeEncryptedFeedCache({
        ciphertextB64: dns.ciphertextB64,
        createdAt: new Date().toISOString(),
        domain: DOMAIN,
      });
      setTelemetry({
        t1: auth.metrics.t1TokenExchangeMs,
        t2: auth.metrics.t2KeyDerivationMs,
        t3: dns.t3DnsRttMs,
        t4: Math.round(performance.now() - t4Start),
        resolver: dns.resolverUsed,
        parts: dns.partCount,
        dnsQueries: dns.dnsQueryCount,
      });
    } catch (e) {
      setError(e instanceof Error ? e.message : "unexpected error");
    } finally {
      setLoading(false);
    }
  }

  async function handleManualRefresh() {
    if (!sessionKey) return;
    setLoading(true);
    setError(null);
    try {
      const dns = await fetchMultipartCiphertext(DOMAIN, "cloudflare");
      const t4Start = performance.now();
      const jsonText = await decryptFeedCiphertext(dns.ciphertextB64, sessionKey);
      setFeed(JSON.parse(jsonText) as FeedItem[]);
      setTelemetry((t) => ({
        ...t,
        t3: dns.t3DnsRttMs,
        t4: Math.round(performance.now() - t4Start),
        resolver: dns.resolverUsed,
        parts: dns.partCount,
        dnsQueries: dns.dnsQueryCount,
      }));
    } catch (e) {
      setError(e instanceof Error ? e.message : "refresh failed");
    } finally {
      setLoading(false);
    }
  }

  return (
    <main style={{ maxWidth: 840, margin: "32px auto", padding: 16, fontFamily: "Inter, sans-serif" }}>
      <h1>TheFeed Reference Shell</h1>
      <p>Invite token login to secure unwrap, DNS chunk fetch, decrypt, and render.</p>

      <section style={{ display: "flex", gap: 8, marginBottom: 18 }}>
        <input
          type="password"
          placeholder="Invite token"
          value={token}
          onChange={(e) => setToken(e.target.value)}
          style={{ flex: 1, padding: 10, borderRadius: 8, border: "1px solid #ccc" }}
        />
        <button onClick={handleTokenSubmit} disabled={loading || !token.trim()}>
          {loading ? "Working..." : "Unlock Feed"}
        </button>
        <button onClick={handleManualRefresh} disabled={loading || !sessionKey}>
          Manual Refresh
        </button>
      </section>

      {cached && !feed.length ? (
        <p style={{ fontSize: 13, color: "#666" }}>
          Encrypted cache available from {cached.createdAt}. Live fetch runs after unlock.
        </p>
      ) : null}

      {error ? <p style={{ color: "#b00020" }}>{error}</p> : null}

      <section style={{ marginTop: 12 }}>
        {feed.map((item, i) => (
          <article key={`${item.source}-${i}`} style={{ border: "1px solid #eee", borderRadius: 8, padding: 12, marginBottom: 8 }}>
            <div style={{ fontSize: 12, color: "#666" }}>
              {item.source} - {item.time}
            </div>
            <div
              style={
                hasRTL(item.text)
                  ? { direction: "rtl", textAlign: "right", unicodeBidi: "plaintext" }
                  : { direction: "ltr", textAlign: "left", unicodeBidi: "plaintext" }
              }
            >
              {item.text}
            </div>
          </article>
        ))}
        {!feed.length && <p style={{ color: "#666" }}>No feed loaded yet.</p>}
      </section>

      <TelemetryOverlay
        t1={telemetry.t1}
        t2={telemetry.t2}
        t3={telemetry.t3}
        t4={telemetry.t4}
        resolver={telemetry.resolver}
        parts={telemetry.parts}
        dnsQueries={telemetry.dnsQueries}
      />
    </main>
  );
}
