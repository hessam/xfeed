export type CachedFeedRecord = {
  ciphertextB64: string;
  createdAt: string;
  domain: string;
};

const CACHE_KEY = "thefeed.cached.messages.v1";

export function writeEncryptedFeedCache(record: CachedFeedRecord): void {
  if (typeof window === "undefined") return;
  sessionStorage.setItem(CACHE_KEY, JSON.stringify(record));
}

export function readEncryptedFeedCache(): CachedFeedRecord | null {
  if (typeof window === "undefined") return null;
  const raw = sessionStorage.getItem(CACHE_KEY);
  if (!raw) return null;
  try {
    return JSON.parse(raw) as CachedFeedRecord;
  } catch {
    return null;
  }
}

export function clearEncryptedFeedCache(): void {
  if (typeof window === "undefined") return;
  sessionStorage.removeItem(CACHE_KEY);
}
