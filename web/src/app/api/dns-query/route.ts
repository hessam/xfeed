import { NextResponse } from "next/server";

const AUTH_BASE = (process.env.NEXT_PUBLIC_AUTH_BASE_URL ?? "https://auth.xtory.sbs").trim();

export async function GET(req: Request) {
  try {
    const { searchParams } = new URL(req.url);
    const name = searchParams.get("name");
    
    if (!name) {
      return NextResponse.json({ Status: 3 }, { status: 400 });
    }

    const res = await fetch(`${AUTH_BASE}/dns-query?name=${encodeURIComponent(name)}`, {
      method: "GET",
      headers: { "Accept": "application/dns-json" },
    });
    
    if (!res.ok) {
      return NextResponse.json({ Status: 3 }, { status: res.status });
    }
    
    const data = await res.json();
    return NextResponse.json(data);
  } catch (error) {
    return NextResponse.json({ Status: 3 }, { status: 500 });
  }
}
