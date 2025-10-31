import { NextRequest, NextResponse } from "next/server";

const ACCESS_TOKEN_COOKIE = "redline_access";
const PROTECTED_PREFIXES = ["/dashboard"];

// Redirects to /login when the access-token cookie is absent. This is a
// presence check only, not a signature/expiry check - the cookie is
// httpOnly so middleware can see it exists but every real request still
// gets validated server-side by the gateway's JWT middleware, which is
// the actual authority on whether the token is valid.
export function middleware(req: NextRequest) {
  const { pathname } = req.nextUrl;
  if (!PROTECTED_PREFIXES.some((p) => pathname.startsWith(p))) {
    return NextResponse.next();
  }
  if (req.cookies.get(ACCESS_TOKEN_COOKIE)) {
    return NextResponse.next();
  }
  const loginURL = new URL("/login", req.url);
  loginURL.searchParams.set("next", pathname);
  return NextResponse.redirect(loginURL);
}

export const config = {
  matcher: ["/dashboard/:path*"],
};
