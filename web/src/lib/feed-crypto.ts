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

async function inflateDeflate(input: Uint8Array): Promise<Uint8Array> {
  if (typeof DecompressionStream === "undefined") {
    return input;
  }
  const ds = new DecompressionStream("deflate");
  const stream = new Blob([toArrayBuffer(input)]).stream().pipeThrough(ds);
  const ab = await new Response(stream).arrayBuffer();
  return new Uint8Array(ab);
}

export async function decryptFeedCiphertext(ciphertextB64: string, sessionKey: string): Promise<string> {
  const raw = b64urlToBytes(ciphertextB64);
  if (raw.length < 13) throw new Error("ciphertext too short");
  const iv = raw.slice(0, 12);
  const body = raw.slice(12);

  const keyBytes = new TextEncoder().encode(sessionKey);
  const normalizedKey = await crypto.subtle.digest("SHA-256", keyBytes);
  const key = await crypto.subtle.importKey("raw", normalizedKey, "AES-GCM", false, ["decrypt"]);
  const clear = await crypto.subtle.decrypt(
    { name: "AES-GCM", iv: toArrayBuffer(iv) },
    key,
    toArrayBuffer(body)
  );

  const inflated = await inflateDeflate(new Uint8Array(clear));
  return new TextDecoder().decode(inflated);
}
