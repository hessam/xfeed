import { NextResponse } from "next/server";

const AUTH_BASE = (process.env.NEXT_PUBLIC_AUTH_BASE_URL ?? "https://auth.xtory.sbs").trim();

export async function POST(req: Request) {
  try {
    const body = await req.json();
    const res = await fetch(`${AUTH_BASE}/v1/token/exchange`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    
    if (!res.ok) {
      return NextResponse.json({ error: "Upstream error" }, { status: res.status });
    }
    
    const data = await res.json();
    return NextResponse.json(data);
  } catch (error) {
    return NextResponse.json({ error: "Proxy error" }, { status: 500 });
  }
}
