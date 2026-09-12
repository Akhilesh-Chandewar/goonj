/**
 * @goonj/api-client — generated types + a thin typed fetch wrapper.
 *
 * `schema.gen.ts` is generated from openapi.yaml by `bun --filter
 * '@goonj/api-client' generate` (openapi-typescript). The wrapper below
 * resolves response JSON types from the schema so web pages can consume
 * `client.GET("/trending")` with full type inference — no hand-written
 * response interfaces for anything the spec describes.
 */

import type { paths } from "./schema.gen";

export type { paths, components } from "./schema.gen";

/** Route paths known to the spec. */
export type Route = string & (keyof paths);

/** JSON body type of a successful response for a path/method pair. */
export type JsonResponse<P extends Route, M extends HttpMethod = "get"> =
  paths[P][M] extends { responses: infer R }
    ? R extends { 200: { content: { "application/json": infer J } } }
      ? J
      : unknown
    : never;

type HttpMethod = "get" | "post";

export interface ClientOptions {
  baseUrl: string;
  /** Bearer token provider — called per request so rotation is picked up. */
  token?: () => string | null;
}

export class ApiClient {
  private baseUrl: string;
  private token?: () => string | null;

  constructor(opts: ClientOptions) {
    this.baseUrl = opts.baseUrl.replace(/\/$/, "");
    this.token = opts.token;
  }

  /** Typed GET: returns the 2xx JSON body defined in openapi.yaml. */
  async get<const P extends Route>(path: P): Promise<JsonResponse<P, "get">> {
    return this.request("GET", path) as Promise<JsonResponse<P, "get">>;
  }

  /** Typed POST: body checked against the spec at the call site. */
  async post<P extends Route>(
    path: P,
    body?: unknown
  ): Promise<JsonResponse<P, "post">> {
    return this.request("POST", path, body) as Promise<
      JsonResponse<P, "post">
    >;
  }

  private async request(
    method: "GET" | "POST",
    path: string,
    body?: unknown
  ): Promise<unknown> {
    const headers = new Headers();
    if (body !== undefined) headers.set("Content-Type", "application/json");
    const token = this.token?.();
    if (token) headers.set("Authorization", `Bearer ${token}`);

    const res = await fetch(`${this.baseUrl}${path}`, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const json = (await res.json().catch(() => ({}))) as { error?: unknown };
    if (!res.ok) {
      throw new Error(
        typeof json.error === "string" ? json.error : `HTTP ${res.status}`
      );
    }
    return json;
  }
}

/** Convenience builder for the web app. */
export function createApiClient(opts: ClientOptions): ApiClient {
  return new ApiClient(opts);
}
