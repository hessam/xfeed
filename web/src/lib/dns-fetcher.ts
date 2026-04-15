export type ResolverName = "cloudflare" | "google";

export type DNSProbeResult = {
  resolver: ResolverName;
  latencyMs: number;
  ok: boolean;
};

export type DNSFetchResult = {
  ciphertextB64: string;
  partCount: number;
  t3DnsRttMs: number;
  resolverUsed: ResolverName;
  dnsQueryCount: number;
};

const RESOLVERS: Record<ResolverName, (name: string) => string> = {
  cloudflare: (name) =>
    `https://cloudflare-dns.com/dns-query?name=${encodeURIComponent(name)}&type=TXT`,
  google: (name) =>
    `https://dns.google/resolve?name=${encodeURIComponent(name)}&type=TXT`,
};
const LOCAL_BRIDGE = process.env.NEXT_PUBLIC_DNS_BRIDGE_URL;

function cleanName(input: string): string {
  return input.trim().replace(/\s+/g, "");
}

type DoHAnswer = { data?: string };
type DoHResponse = { Status?: number; Answer?: DoHAnswer[] };

export async function probeResolvers(domain: string): Promise<DNSProbeResult[]> {
  domain = cleanName(domain);
  if (LOCAL_BRIDGE) {
    const start = performance.now();
    try {
      await fetchTXTViaBridge(`warmup.${domain}`);
      return [{ resolver: "cloudflare", latencyMs: Math.round(performance.now() - start), ok: true }];
    } catch {
      return [{ resolver: "cloudflare", latencyMs: Math.round(performance.now() - start), ok: false }];
    }
  }
  const probes = (Object.keys(RESOLVERS) as ResolverName[]).map(async (resolver) => {
    const qname = `warmup.${domain}`;
    const start = performance.now();
    try {
      await fetchTXT(resolver, qname);
      return { resolver, latencyMs: Math.round(performance.now() - start), ok: true };
    } catch {
      return { resolver, latencyMs: Math.round(performance.now() - start), ok: false };
    }
  });
  return Promise.all(probes);
}

export async function fetchMultipartCiphertext(
  domain: string,
  preferred: ResolverName
): Promise<DNSFetchResult> {
  domain = cleanName(domain);
  const qname = `p1.msg.${domain}`;
  const t3Start = performance.now();
  let dnsQueryCount = 0;
  const firstScatter = await fetchTXTScatter(preferred, qname);
  dnsQueryCount += firstScatter.attempted;
  const first = firstScatter.txt;
  const firstParsed = parseChunkHeader(first);
  if (!firstParsed) {
    throw new Error("invalid TXT chunk header");
  }

  const parts: string[] = new Array(firstParsed.total).fill("");
  parts[firstParsed.part - 1] = firstParsed.payload;
  for (let i = 2; i <= firstParsed.total; i++) {
    const partQname = `p${i}.msg.${domain}`;
    const scatter = await fetchTXTScatter(preferred, partQname);
    dnsQueryCount += scatter.attempted;
    const txt = scatter.txt;
    const parsed = parseChunkHeader(txt);
    if (!parsed || parsed.part !== i || parsed.total !== firstParsed.total) {
      throw new Error(`invalid chunk ${i}/${firstParsed.total}`);
    }
    parts[i - 1] = parsed.payload;
  }

  return {
    ciphertextB64: parts.join(""),
    partCount: firstParsed.total,
    t3DnsRttMs: Math.round(performance.now() - t3Start),
    resolverUsed: preferred,
    dnsQueryCount,
  };
}

async function fetchTXT(resolver: ResolverName, qname: string): Promise<string> {
  qname = cleanName(qname);
  if (LOCAL_BRIDGE) {
    return fetchTXTViaBridge(qname);
  }
  const res = await fetch(RESOLVERS[resolver](qname), {
    headers: { accept: "application/dns-json" },
  });
  if (!res.ok) throw new Error(`resolver ${resolver} failed`);
  const body = (await res.json()) as DoHResponse;
  if (body.Status !== 0 || !body.Answer?.length) {
    throw new Error(`no TXT answer from ${resolver}`);
  }
  const txt = body.Answer.find((a) => a.data?.includes("c:"))?.data ?? body.Answer[0].data ?? "";
  return txt.replace(/^"|"$/g, "");
}

async function fetchTXTScatter(_preferred: ResolverName, qname: string): Promise<{ txt: string; attempted: number }> {
  const order: ResolverName[] = ["google", "cloudflare"];
  const maxRetries = 3;
  let totalAttempted = 0;
  for (let attempt = 0; attempt < maxRetries; attempt++) {
    try {
      const jobs = order.map((r) => fetchTXT(r, qname));
      totalAttempted += jobs.length;
      const txt = await Promise.any(jobs);
      return { txt, attempted: totalAttempted };
    } catch {
      if (attempt < maxRetries - 1) {
        await new Promise((r) => setTimeout(r, 2000));
      }
    }
  }
  throw new Error(`DNS lookup failed after ${maxRetries} retries for ${qname}`);
}

async function fetchTXTViaBridge(qname: string): Promise<string> {
  const res = await fetch(`${LOCAL_BRIDGE}?name=${encodeURIComponent(qname)}`);
  if (!res.ok) throw new Error("local DNS bridge unavailable");
  const body = (await res.json()) as { txt?: string };
  if (!body.txt) throw new Error("empty bridge TXT response");
  return body.txt;
}

function parseChunkHeader(txt: string): { part: number; total: number; payload: string } | null {
  const m = txt.match(/^c:(\d+)\/(\d+):([A-Za-z0-9\-_]+)\./);
  if (!m) return null;
  return {
    part: Number(m[1]),
    total: Number(m[2]),
    payload: m[3],
  };
}
