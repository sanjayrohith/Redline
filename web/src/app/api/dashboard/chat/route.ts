import { NextRequest, NextResponse } from "next/server";
import { gatewayBaseURL } from "@/lib/config";

// Proxies POST /v1/dashboard/chat/completions, streaming the gateway's SSE
// response body straight through rather than buffering it - buffering
// would defeat the whole point of a streaming chat playground.
export async function POST(req: NextRequest) {
  const body = await req.text();

  const gatewayRes = await fetch(`${gatewayBaseURL()}/v1/dashboard/chat/completions`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Cookie: req.headers.get("cookie") ?? "",
    },
    body,
  });

  return new NextResponse(gatewayRes.body, {
    status: gatewayRes.status,
    headers: { "Content-Type": gatewayRes.headers.get("Content-Type") ?? "application/json" },
  });
}
