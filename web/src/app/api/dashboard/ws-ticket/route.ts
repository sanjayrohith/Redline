import { NextRequest, NextResponse } from "next/server";

// Hands the browser its own httpOnly access-token cookie value back, for
// the one purpose a browser genuinely needs it as a JS-visible string: a
// WebSocket connection to the gateway's own origin, which cannot carry a
// custom Authorization header or see a cookie that was only ever set for
// this app's origin. The token is already short-lived (15 minutes); this
// route does not mint anything new, only reads what the request already
// carries server-side.
export async function GET(req: NextRequest) {
  const token = req.cookies.get("redline_access")?.value;
  if (!token) {
    return NextResponse.json({ error: "not authenticated" }, { status: 401 });
  }
  return NextResponse.json({ token });
}
