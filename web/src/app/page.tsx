"use client";

import { useEffect, useState, useMemo, useCallback } from "react";
import { readEncryptedFeedCache, writeEncryptedFeedCache } from "../../lib/cache";
import { probeResolvers, fetchMultipartCiphertext, type ResolverName } from "../lib/dns-fetcher";
import { decryptFeedCiphertext } from "../lib/feed-crypto";
import { exchangeAndUnwrapToken } from "../../lib/token-flow";

type FeedItem = { source: string; text: string; time: string };
type FilterType = "all" | "x" | "tg" | "rss";

const AUTH_BASE = (process.env.NEXT_PUBLIC_AUTH_BASE_URL ?? "https://auth.example.com").trim();
const DOMAIN = (process.env.NEXT_PUBLIC_DEFAULT_DOMAIN ?? "t.example.com").trim();

function hasRTL(text: string): boolean {
  return /[\u0590-\u08FF]/.test(text);
}

function getSourceType(source: string): "x" | "tg" | "rss" {
  if (source.startsWith("x:")) return "x";
  if (source.startsWith("tg:")) return "tg";
  return "rss";
}

function getSourceHandle(source: string): string {
  if (source.startsWith("x:")) return "@" + source.slice(2);
  if (source.startsWith("tg:")) return source.slice(3);
  return source;
}

function formatTimeAgo(timeStr: string): string {
  try {
    const date = new Date(timeStr);
    if (isNaN(date.getTime())) return timeStr;
    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffMin = Math.floor(diffMs / 60000);
    if (diffMin < 1) return "just now";
    if (diffMin < 60) return `${diffMin}m ago`;
    const diffH = Math.floor(diffMin / 60);
    if (diffH < 24) return `${diffH}h ago`;
    const diffD = Math.floor(diffH / 24);
    if (diffD < 7) return `${diffD}d ago`;
    return date.toLocaleDateString("en-US", { month: "short", day: "numeric" });
  } catch {
    return timeStr;
  }
}

function groupByDate(items: FeedItem[]): { label: string; items: FeedItem[] }[] {
  const groups: Record<string, FeedItem[]> = {};
  const now = new Date();
  const todayStr = now.toDateString();
  const yesterday = new Date(now);
  yesterday.setDate(yesterday.getDate() - 1);
  const yesterdayStr = yesterday.toDateString();

  for (const item of items) {
    let label: string;
    try {
      const d = new Date(item.time);
      if (isNaN(d.getTime())) {
        label = "Recent";
      } else if (d.toDateString() === todayStr) {
        label = "Today";
      } else if (d.toDateString() === yesterdayStr) {
        label = "Yesterday";
      } else {
        label = d.toLocaleDateString("en-US", { weekday: "long", month: "short", day: "numeric" });
      }
    } catch {
      label = "Recent";
    }
    if (!groups[label]) groups[label] = [];
    groups[label].push(item);
  }

  return Object.entries(groups).map(([label, items]) => ({ label, items }));
}

// SVG icons
const XIcon = () => (
  <svg viewBox="0 0 24 24" fill="currentColor">
    <path d="M18.244 2.25h3.308l-7.227 8.26 8.502 11.24H16.17l-5.214-6.817L4.99 21.75H1.68l7.73-8.835L1.254 2.25H8.08l4.713 6.231zm-1.161 17.52h1.833L7.084 4.126H5.117z"/>
  </svg>
);

const TelegramIcon = () => (
  <svg viewBox="0 0 24 24" fill="currentColor">
    <path d="M11.944 0A12 12 0 0 0 0 12a12 12 0 0 0 12 12 12 12 0 0 0 12-12A12 12 0 0 0 12 0a12 12 0 0 0-.056 0zm4.962 7.224c.1-.002.321.023.465.14a.506.506 0 0 1 .171.325c.016.093.036.306.02.472-.18 1.898-.962 6.502-1.36 8.627-.168.9-.499 1.201-.82 1.23-.696.065-1.225-.46-1.9-.902-1.056-.693-1.653-1.124-2.678-1.8-1.185-.78-.417-1.21.258-1.91.177-.184 3.247-2.977 3.307-3.23.007-.032.014-.15-.056-.212s-.174-.041-.249-.024c-.106.024-1.793 1.14-5.061 3.345-.479.33-.913.49-1.302.48-.428-.008-1.252-.241-1.865-.44-.752-.245-1.349-.374-1.297-.789.027-.216.325-.437.893-.663 3.498-1.524 5.83-2.529 6.998-3.014 3.332-1.386 4.025-1.627 4.476-1.635z"/>
  </svg>
);

const RssIcon = () => (
  <svg viewBox="0 0 24 24" fill="currentColor">
    <circle cx="6.18" cy="17.82" r="2.18"/>
    <path d="M4 4.44v2.83c7.03 0 12.73 5.7 12.73 12.73h2.83c0-8.59-6.97-15.56-15.56-15.56zm0 5.66v2.83c3.9 0 7.07 3.17 7.07 7.07h2.83c0-5.47-4.43-9.9-9.9-9.9z"/>
  </svg>
);

const RefreshIcon = () => (
  <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <polyline points="23 4 23 10 17 10"/>
    <path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/>
  </svg>
);

// Skeleton component
function SkeletonCard({ index }: { index: number }) {
  return (
    <div className="post-card skeleton-card" style={{ animationDelay: `${index * 60}ms` }}>
      <div className="post-header">
        <div className="skeleton-pill" style={{ width: 72, height: 22 }} />
        <div className="skeleton-pill" style={{ width: 100, height: 16 }} />
        <div className="skeleton-pill" style={{ width: 50, height: 14, marginLeft: "auto" }} />
      </div>
      <div className="skeleton-lines">
        <div className="skeleton-line" style={{ width: "100%" }} />
        <div className="skeleton-line" style={{ width: "85%" }} />
        <div className="skeleton-line" style={{ width: "60%" }} />
      </div>
    </div>
  );
}

function FeedSkeleton() {
  return (
    <div className="feed-list">
      <div className="feed-date-group">
        <div className="skeleton-pill" style={{ width: 60, height: 14, marginBottom: 12 }} />
        {Array.from({ length: 6 }).map((_, i) => (
          <SkeletonCard key={i} index={i} />
        ))}
      </div>
    </div>
  );
}

export default function HomePage() {
  const [token, setToken] = useState("");
  const [sessionKey, setSessionKey] = useState<string | null>(null);
  const [feed, setFeed] = useState<FeedItem[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [initialLoading, setInitialLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [filter, setFilter] = useState<FilterType>("all");
  const [telemetry, setTelemetry] = useState<{
    t1?: number; t2?: number; t3?: number; t4?: number;
    resolver?: string; parts?: number; dnsQueries?: number;
  }>({});

  const [cached, setCached] = useState<ReturnType<typeof readEncryptedFeedCache>>(null);
  useEffect(() => { setCached(readEncryptedFeedCache()); }, []);

  // Sort feed by time descending
  const sortedFeed = useMemo(() => {
    return [...feed].sort((a, b) => {
      try {
        return new Date(b.time).getTime() - new Date(a.time).getTime();
      } catch {
        return 0;
      }
    });
  }, [feed]);

  // Filter feed
  const filteredFeed = useMemo(() => {
    if (filter === "all") return sortedFeed;
    return sortedFeed.filter(item => getSourceType(item.source) === filter);
  }, [sortedFeed, filter]);

  // Count by source
  const counts = useMemo(() => {
    const c = { all: feed.length, x: 0, tg: 0, rss: 0 };
    for (const item of feed) {
      const t = getSourceType(item.source);
      c[t]++;
    }
    return c;
  }, [feed]);

  const dateGroups = useMemo(() => groupByDate(filteredFeed), [filteredFeed]);

  const fetchFeed = useCallback(async (key: string, resolver: ResolverName) => {
    const dns = await fetchMultipartCiphertext(DOMAIN, resolver);
    const t4Start = performance.now();
    const jsonText = await decryptFeedCiphertext(dns.ciphertextB64, key);
    const parsed = JSON.parse(jsonText) as FeedItem[];
    setFeed(parsed);
    writeEncryptedFeedCache({
      ciphertextB64: dns.ciphertextB64,
      createdAt: new Date().toISOString(),
      domain: DOMAIN,
    });
    return { dns, t4: Math.round(performance.now() - t4Start) };
  }, []);

  async function handleTokenSubmit() {
    setLoading(true);
    setError(null);
    try {
      const probeTask = probeResolvers(DOMAIN);
      const auth = await exchangeAndUnwrapToken(AUTH_BASE, token.trim());

      // Auth succeeded — show feed view with loading skeleton
      setSessionKey(auth.sessionKey);
      setInitialLoading(true);

      const probe = await probeTask;
      const fastest: ResolverName =
        probe.filter(p => p.ok).sort((a, b) => a.latencyMs - b.latencyMs)[0]?.resolver ?? "cloudflare";

      const result = await fetchFeed(auth.sessionKey, fastest);
      setTelemetry({
        t1: auth.metrics.t1TokenExchangeMs,
        t2: auth.metrics.t2KeyDerivationMs,
        t3: result.dns.t3DnsRttMs,
        t4: result.t4,
        resolver: result.dns.resolverUsed,
        parts: result.dns.partCount,
        dnsQueries: result.dns.dnsQueryCount,
      });
    } catch (e) {
      const msg = e instanceof Error ? e.message : "unexpected error";
      setError(msg);
      // If auth failed, revert to login
      if (msg.includes("token exchange failed") || msg.includes("unauthorized")) {
        setSessionKey(null);
      }
    } finally {
      setLoading(false);
      setInitialLoading(false);
    }
  }

  async function handleRefresh() {
    if (!sessionKey) return;
    setRefreshing(true);
    setError(null);
    try {
      const result = await fetchFeed(sessionKey, "cloudflare");
      setTelemetry(t => ({
        ...t,
        t3: result.dns.t3DnsRttMs,
        t4: result.t4,
        resolver: result.dns.resolverUsed,
        parts: result.dns.partCount,
        dnsQueries: result.dns.dnsQueryCount,
      }));
    } catch (e) {
      setError(e instanceof Error ? e.message : "refresh failed");
    } finally {
      setRefreshing(false);
    }
  }

  // Auto-refresh every 2 minutes
  useEffect(() => {
    if (!sessionKey) return;
    const interval = setInterval(handleRefresh, 120_000);
    return () => clearInterval(interval);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionKey]);

  // Login view
  if (!sessionKey) {
    return (
      <div className="login-screen">
        <div className="login-card">
          <div className="login-logo">Xfeed</div>
          <div className="login-subtitle">
            Encrypted feed reader. Enter your invite token to unlock the feed.
          </div>
          <div className="input-group">
            <input
              className="input-field"
              type="password"
              placeholder="Paste your invite token"
              value={token}
              onChange={e => setToken(e.target.value)}
              onKeyDown={e => e.key === "Enter" && token.trim() && handleTokenSubmit()}
              autoFocus
            />
          </div>
          <button
            className="btn-primary"
            onClick={handleTokenSubmit}
            disabled={loading || !token.trim()}
          >
            {loading ? "Decrypting..." : "Unlock Feed"}
          </button>
          {error && <div className="error-msg">{error}</div>}
          {cached && (
            <div style={{ marginTop: 14, fontSize: 12, color: "var(--text-muted)", textAlign: "center" }}>
              Encrypted cache available from {new Date(cached.createdAt).toLocaleString()}
            </div>
          )}
        </div>
      </div>
    );
  }

  // Feed view
  return (
    <div className="feed-container">
      {/* Header */}
      <header className="feed-header">
        <div className="feed-header-inner">
          <div className="feed-logo">Xfeed</div>
          <div className="feed-actions">
            <button
              className={`btn-icon ${refreshing ? "spinning" : ""}`}
              onClick={handleRefresh}
              disabled={refreshing}
              title="Refresh feed"
            >
              <RefreshIcon />
            </button>
          </div>
        </div>
      </header>

      {/* Stats */}
      {!initialLoading && (
        <div className="stats-bar">
          <div className="stat-item">
            <span className="stat-dot" />
            <span>Live</span>
          </div>
          <span>·</span>
          <span>{feed.length} posts from {new Set(feed.map(f => f.source)).size} sources</span>
          <span style={{ marginLeft: "auto" }}>
            {telemetry.t3 ? `${telemetry.t3}ms DNS` : ""}
          </span>
        </div>
      )}

      {/* Filter chips */}
      {!initialLoading && feed.length > 0 && (
        <div className="filter-bar">
          <button
            className={`filter-chip ${filter === "all" ? "active" : ""}`}
            onClick={() => setFilter("all")}
          >
            All <span className="chip-count">{counts.all}</span>
          </button>
          <button
            className={`filter-chip ${filter === "x" ? "active" : ""}`}
            onClick={() => setFilter("x")}
          >
            <span className="chip-dot" style={{ background: "var(--x-color)" }} />
            X / Twitter <span className="chip-count">{counts.x}</span>
          </button>
          <button
            className={`filter-chip ${filter === "tg" ? "active" : ""}`}
            onClick={() => setFilter("tg")}
          >
            <span className="chip-dot" style={{ background: "var(--tg-color)" }} />
            Telegram <span className="chip-count">{counts.tg}</span>
          </button>
          {counts.rss > 0 && (
            <button
              className={`filter-chip ${filter === "rss" ? "active" : ""}`}
              onClick={() => setFilter("rss")}
            >
              <span className="chip-dot" style={{ background: "var(--rss-color)" }} />
              RSS <span className="chip-count">{counts.rss}</span>
            </button>
          )}
        </div>
      )}

      {error && (
        <div style={{ maxWidth: 720, margin: "12px auto", padding: "0 24px" }}>
          <div className="error-msg">{error}</div>
        </div>
      )}

      {/* Loading skeleton */}
      {initialLoading && <FeedSkeleton />}

      {/* Feed */}
      {!initialLoading && (
        <div className="feed-list">
          {filteredFeed.length === 0 ? (
            <div className="empty-state">
              <div className="empty-state-icon">📡</div>
              <div className="empty-state-title">
                {feed.length === 0 ? "Feed is loading..." : "No posts match this filter"}
              </div>
              <div className="empty-state-sub">
                {feed.length === 0
                  ? "The server may still be fetching posts. Try refreshing."
                  : "Try selecting a different source"}
              </div>
              {feed.length === 0 && (
                <button
                  className="btn-primary"
                  style={{ marginTop: 16, width: "auto", padding: "10px 24px" }}
                  onClick={handleRefresh}
                  disabled={refreshing}
                >
                  {refreshing ? "Refreshing..." : "Retry"}
                </button>
              )}
            </div>
          ) : (
            dateGroups.map(group => (
              <div key={group.label} className="feed-date-group">
                <div className="feed-date-label">{group.label}</div>
                {group.items.map((item, i) => {
                  const type = getSourceType(item.source);
                  const isRtl = hasRTL(item.text);
                  return (
                    <article
                      key={`${item.source}-${item.time}-${i}`}
                      className="post-card"
                      style={{ animationDelay: `${i * 30}ms` }}
                    >
                      <div className="post-header">
                        <span className={`source-badge ${type}-badge`}>
                          {type === "x" && <XIcon />}
                          {type === "tg" && <TelegramIcon />}
                          {type === "rss" && <RssIcon />}
                          {type === "x" ? "X" : type === "tg" ? "Telegram" : "RSS"}
                        </span>
                        <span className="source-handle">{getSourceHandle(item.source)}</span>
                        <span className="post-time">{formatTimeAgo(item.time)}</span>
                      </div>
                      <div className={`post-body ${isRtl ? "rtl" : "ltr"}`}>
                        {item.text}
                      </div>
                    </article>
                  );
                })}
              </div>
            ))
          )}
        </div>
      )}

      {/* Telemetry */}
      {telemetry.t1 != null && (
        <div className="telemetry-overlay">
          <div><span className="tel-label">Auth </span><span className="tel-value">{telemetry.t1}ms</span></div>
          <div><span className="tel-label">Key  </span><span className="tel-value">{telemetry.t2}ms</span></div>
          <div><span className="tel-label">DNS  </span><span className="tel-value">{telemetry.t3}ms</span></div>
          <div><span className="tel-label">Rend </span><span className="tel-value">{telemetry.t4}ms</span></div>
          <div><span className="tel-label">Via  </span><span className="tel-value">{telemetry.resolver}</span></div>
          <div><span className="tel-label">Parts</span> <span className="tel-value">{telemetry.parts} ({telemetry.dnsQueries} q)</span></div>
        </div>
      )}
    </div>
  );
}
