import { NextRequest, NextResponse } from "next/server";
import { gatewayBaseURL } from "@/lib/config";

export async function POST(req: NextRequest) {
  const body = await req.text();
  const res = await fetch(`${gatewayBaseURL()}/v1/dashboard/benchmarks`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Cookie: req.headers.get("cookie") ?? "",
    },
    body,
  });
  return new NextResponse(await res.text(), {
    status: res.status,
    headers: { "Content-Type": "application/json" },
  });
}

export async function GET(req: NextRequest) {
  const modelID = req.nextUrl.searchParams.get("model_id") ?? "";
  const res = await fetch(`${gatewayBaseURL()}/v1/dashboard/benchmarks?model_id=${encodeURIComponent(modelID)}`, {
    headers: { Cookie: req.headers.get("cookie") ?? "" },
  });
  return new NextResponse(await res.text(), {
    status: res.status,
    headers: { "Content-Type": "application/json" },
  });
}
