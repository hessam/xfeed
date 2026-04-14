"use client";

type Props = {
  t1?: number;
  t2?: number;
  t3?: number;
  t4?: number;
  resolver?: string;
  parts?: number;
  dnsQueries?: number;
};

export function TelemetryOverlay({ t1, t2, t3, t4, resolver, parts, dnsQueries }: Props) {
  return (
    <div
      style={{
        position: "fixed",
        right: 16,
        bottom: 16,
        padding: "10px 12px",
        borderRadius: 8,
        background: "rgba(20,20,20,0.88)",
        color: "#fff",
        fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
        fontSize: 12,
        lineHeight: 1.5,
        zIndex: 1000,
      }}
    >
      <div>T1 Auth RTT: {t1 ?? "-"}ms</div>
      <div>T2 Unwrap: {t2 ?? "-"}ms</div>
      <div>T3 DNS RTT: {t3 ?? "-"}ms</div>
      <div>T4 Decode+Render: {t4 ?? "-"}ms</div>
      <div>Resolver: {resolver ?? "-"}</div>
      <div>Parts: {parts ?? "-"}</div>
      <div>DNS queries: {dnsQueries ?? "-"}</div>
    </div>
  );
}
