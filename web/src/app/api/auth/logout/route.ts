import { NextRequest, NextResponse } from "next/server";
import { gatewayBaseURL } from "@/lib/config";

export async function POST(req: NextRequest) {
  const gatewayRes = await fetch(`${gatewayBaseURL()}/v1/auth/logout`, {
    method: "POST",
    headers: { Cookie: req.headers.get("cookie") ?? "" },
  });

  const res = new NextResponse(null, { status: gatewayRes.status });
  for (const cookie of gatewayRes.headers.getSetCookie()) {
    res.headers.append("Set-Cookie", cookie);
  }
  return res;
}
