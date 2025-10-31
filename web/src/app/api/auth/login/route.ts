import { NextRequest, NextResponse } from "next/server";
import { gatewayBaseURL } from "@/lib/config";

// Proxies to the gateway's POST /v1/auth/login and forwards its Set-Cookie
// headers verbatim, so the httpOnly session cookies land on this app's own
// origin rather than the gateway's - the browser never talks to the
// gateway directly, and page JavaScript never sees the tokens.
export async function POST(req: NextRequest) {
  const body = await req.text();

  const gatewayRes = await fetch(`${gatewayBaseURL()}/v1/auth/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body,
  });

  const res = new NextResponse(gatewayRes.ok ? null : await gatewayRes.text(), {
    status: gatewayRes.status,
  });
  for (const cookie of gatewayRes.headers.getSetCookie()) {
    res.headers.append("Set-Cookie", cookie);
  }
  return res;
}
