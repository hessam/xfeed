function b64urlToBytes(input: string): Uint8Array {
  const padded = input.replace(/-/g, "+").replace(/_/g, "/") + "===".slice((input.length + 3) % 4);
  const raw = atob(padded);
  const out = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}

function toArrayBuffer(bytes: Uint8Array): ArrayBuffer {
  return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer;
}

export async function decryptSessionEnvelope(
  inviteToken: string,
  envelopeB64: string,
  ivB64: string,
  saltB64: string,
  kdfIterations = 120000
): Promise<{ key: string; expiresAt: string }> {
  const salt = b64urlToBytes(saltB64);
  const iv = b64urlToBytes(ivB64);
  const envelope = b64urlToBytes(envelopeB64);

  const tokenBytes = new TextEncoder().encode(inviteToken);
  const baseKey = await crypto.subtle.importKey("raw", toArrayBuffer(tokenBytes), "PBKDF2", false, ["deriveKey"]);
  const cryptoKey = await crypto.subtle.deriveKey(
    { name: "PBKDF2", hash: "SHA-256", salt: toArrayBuffer(salt), iterations: kdfIterations },
    baseKey,
    { name: "AES-GCM", length: 256 },
    false,
    ["decrypt"]
  );
  const clear = await crypto.subtle.decrypt(
    { name: "AES-GCM", iv: toArrayBuffer(iv) },
    cryptoKey,
    toArrayBuffer(envelope)
  );
  const text = new TextDecoder().decode(clear);
  const [key, expiresAt] = text.split("|");
  return { key, expiresAt };
}
