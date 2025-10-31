import { NextRequest, NextResponse } from "next/server";
import { gatewayBaseURL } from "@/lib/config";

// Proxies to POST /v1/auth/refresh, forwarding the browser's own cookies
// upstream (the gateway reads the refresh token from them) and relaying
// the rotated Set-Cookie pair back.
export async function POST(req: NextRequest) {
  const gatewayRes = await fetch(`${gatewayBaseURL()}/v1/auth/refresh`, {
    method: "POST",
    headers: { Cookie: req.headers.get("cookie") ?? "" },
  });

  const res = new NextResponse(null, { status: gatewayRes.status });
  for (const cookie of gatewayRes.headers.getSetCookie()) {
    res.headers.append("Set-Cookie", cookie);
  }
  return res;
}
