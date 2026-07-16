import { useState } from "react";
import type { FC } from "react";
import { Button, Space, Tag, Typography } from "antd";

const probes = [
  { label: "API gateway", path: "/healthz" },
  { label: "IAM bootstrap", path: "/iam/v1/me" },
  { label: "VPC networks", path: "/vpc/v1/networks" },
  { label: "Compute instances", path: "/compute/v1/instances" },
  { label: "Kratos", path: "/.ory/kratos/public/health/ready" },
  { label: "Hydra", path: "/.ory/hydra/public/health/ready" },
];

type ProbeResult = {
  state: "idle" | "loading" | "ok" | "auth" | "error";
  status?: number;
  detail?: string;
};

export const ReachabilityPage: FC = () => {
  const [results, setResults] = useState<Record<string, ProbeResult>>({});

  const runProbe = async (path: string) => {
    setResults((prev) => ({ ...prev, [path]: { state: "loading" } }));
    try {
      const res = await fetch(path, { credentials: "include" });
      const text = await res.text();
      const detail = text ? summarizeBody(text) : res.statusText;
      const state = res.ok ? "ok" : res.status === 401 || res.status === 403 ? "auth" : "error";
      setResults((prev) => ({ ...prev, [path]: { state, status: res.status, detail } }));
    } catch {
      setResults((prev) => ({ ...prev, [path]: { state: "error", detail: "request failed" } }));
    }
  };

  return (
    <section className="workbench">
      <div className="panel-heading">
        <div>
          <Typography.Title level={3}>API reachability</Typography.Title>
          <Typography.Text type="secondary">
            These calls use relative URLs. Vite proxies them to the kind stack.
          </Typography.Text>
        </div>
        <Button
          type="primary"
          onClick={() => {
            void Promise.all(probes.map((p) => runProbe(p.path)));
          }}
        >
          Probe all
        </Button>
      </div>

      <div className="probe-grid">
        {probes.map((probe) => {
          const result = results[probe.path] ?? { state: "idle" };
          return (
            <div className="probe-row" key={probe.path}>
              <div>
                <Typography.Text strong>{probe.label}</Typography.Text>
                <Typography.Text code>{probe.path}</Typography.Text>
                {result.detail ? <Typography.Text type="secondary">{result.detail}</Typography.Text> : null}
              </div>
              <Space>
                <Tag color={probeColor(result.state)}>
                  {result.status ? `${result.state} ${result.status}` : result.state}
                </Tag>
                <Button size="small" onClick={() => void runProbe(probe.path)}>
                  Probe
                </Button>
              </Space>
            </div>
          );
        })}
      </div>
    </section>
  );
};

const probeColor = (state: ProbeResult["state"]) => {
  if (state === "ok") return "success";
  if (state === "auth") return "warning";
  if (state === "error") return "error";
  if (state === "loading") return "processing";
  return "default";
};

const summarizeBody = (text: string) => {
  try {
    const parsed = JSON.parse(text) as { message?: unknown; error?: { message?: unknown } };
    const message = parsed.message ?? parsed.error?.message;
    if (typeof message === "string" && message) return message;
  } catch {
    // ignore non-json bodies
  }
  return text.length > 120 ? `${text.slice(0, 117)}...` : text;
};
