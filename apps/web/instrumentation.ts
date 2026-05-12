// Next.js auto-loads this file once per server instance before the first
// request. We use @vercel/otel which wires the OpenTelemetry SDK with a
// sensible default config — fetch instrumentation, W3C trace-context
// propagation, OTLP exporter via the OTEL_EXPORTER_OTLP_ENDPOINT env.
//
// In dev:   OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
// In docker: OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318
//
// If the env is not set, @vercel/otel falls back to a no-op exporter so
// the app still boots without an observability stack.
import { registerOTel } from "@vercel/otel";

export function register() {
  registerOTel({
    serviceName: process.env.OTEL_SERVICE_NAME ?? "nexis-web",
  });
}
