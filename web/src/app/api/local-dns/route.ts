import { NextRequest, NextResponse } from "next/server";
import dgram from "node:dgram";

export const runtime = "nodejs";

const DNS_HOST = process.env.LOCAL_DNS_SERVER_HOST ?? "127.0.0.1";
const DNS_PORT = Number(process.env.LOCAL_DNS_SERVER_PORT ?? "1053");

export async function GET(req: NextRequest) {
  const name = req.nextUrl.searchParams.get("name");
  if (!name) {
    return NextResponse.json({ error: "name is required" }, { status: 400 });
  }

  try {
    const msg = buildTXTQuery(name);
    const resp = await udpQuery(msg);
    const txt = parseFirstTXT(resp);
    if (!txt) {
      return NextResponse.json({ error: "no txt answer" }, { status: 502 });
    }
    return NextResponse.json({ txt });
  } catch (err) {
    return NextResponse.json({ error: (err as Error).message }, { status: 502 });
  }
}

function buildTXTQuery(name: string): Buffer {
  const id = Math.floor(Math.random() * 65535);
  const header = Buffer.alloc(12);
  header.writeUInt16BE(id, 0);
  header.writeUInt16BE(0x0100, 2);
  header.writeUInt16BE(1, 4);

  const labels = name
    .split(".")
    .map((l) => l.trim())
    .filter(Boolean);
  const parts: Buffer[] = [header];
  for (const l of labels) {
    const b = Buffer.from(l, "ascii");
    parts.push(Buffer.from([b.length]));
    parts.push(b);
  }
  parts.push(Buffer.from([0x00]));

  const qtypeQclass = Buffer.alloc(4);
  qtypeQclass.writeUInt16BE(16, 0); // TXT
  qtypeQclass.writeUInt16BE(1, 2); // IN
  parts.push(qtypeQclass);
  return Buffer.concat(parts);
}

function udpQuery(query: Buffer): Promise<Buffer> {
  return new Promise((resolve, reject) => {
    const sock = dgram.createSocket("udp4");
    const timeout = setTimeout(() => {
      sock.close();
      reject(new Error("dns timeout"));
    }, 3000);
    sock.on("error", (e) => {
      clearTimeout(timeout);
      sock.close();
      reject(e);
    });
    sock.on("message", (msg) => {
      clearTimeout(timeout);
      sock.close();
      resolve(msg);
    });
    sock.send(query, DNS_PORT, DNS_HOST, (e) => {
      if (e) {
        clearTimeout(timeout);
        sock.close();
        reject(e);
      }
    });
  });
}

function parseFirstTXT(packet: Buffer): string | null {
  if (packet.length < 12) return null;
  const qdcount = packet.readUInt16BE(4);
  const ancount = packet.readUInt16BE(6);
  if (qdcount < 1 || ancount < 1) return null;

  let i = 12;
  while (i < packet.length && packet[i] !== 0) i += packet[i] + 1;
  i += 1 + 4; // zero terminator + qtype/qclass
  if (i + 12 > packet.length) return null;

  i += 2; // name ptr
  const rtype = packet.readUInt16BE(i);
  i += 2 + 2 + 4;
  const rdlen = packet.readUInt16BE(i);
  i += 2;
  if (rtype !== 16 || i + rdlen > packet.length || rdlen < 1) return null;
  const txtLen = packet[i];
  if (i + 1 + txtLen > packet.length) return null;
  return packet.subarray(i + 1, i + 1 + txtLen).toString("utf-8");
}
