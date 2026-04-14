import { decryptSessionEnvelope } from "./session-envelope";

export type TokenExchangeResponse = {
  envelope: string;
  iv: string;
  salt: string;
  alg: string;
  kdf?: string;
  kdf_iterations?: number;
  expires_in_seconds: number;
};

export type ColdStartMetrics = {
  t1TokenExchangeMs: number;
  t2KeyDerivationMs: number;
};

export async function exchangeAndUnwrapToken(
  authBaseUrl: string,
  inviteToken: string
): Promise<{ sessionKey: string; expiresAt: string; metrics: ColdStartMetrics }> {
  const t1Start = performance.now();
  const res = await fetch(`${authBaseUrl}/v1/token/exchange`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ invite_token: inviteToken }),
  });
  const t1End = performance.now();
  if (!res.ok) {
    throw new Error(`token exchange failed (${res.status})`);
  }
  const payload = (await res.json()) as TokenExchangeResponse;

  const t2Start = performance.now();
  const unwrapped = await decryptSessionEnvelope(
    inviteToken,
    payload.envelope,
    payload.iv,
    payload.salt,
    payload.kdf_iterations ?? 120000
  );
  const t2End = performance.now();

  return {
    sessionKey: unwrapped.key,
    expiresAt: unwrapped.expiresAt,
    metrics: {
      t1TokenExchangeMs: Math.round(t1End - t1Start),
      t2KeyDerivationMs: Math.round(t2End - t2Start),
    },
  };
}
