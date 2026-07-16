// IpamUtilizationBar — NetBox-style визуализация утилизации IP-блока.
//
// Используется на:
//   - AddressPoolDetailPage  (admin: utilization pool + per-CIDR breakdown)
//   - SubnetDetailPage       (admin: utilization subnet)

interface Props {
  total: number | string;
  used: number | string;
  free?: number | string;
  percent?: number;
  label?: string;
  className?: string;
}

export function IpamUtilizationBar({ total, used, free, percent, label, className }: Props) {
  const totalN = Number(total);
  const usedN = Number(used);
  const freeN = free !== undefined ? Number(free) : Math.max(0, totalN - usedN);
  const pct =
    percent !== undefined ? Math.max(0, Math.min(100, percent)) : totalN > 0 ? Math.floor((usedN * 100) / totalN) : 0;

  const color = pct >= 90 ? "bg-red-500" : pct >= 70 ? "bg-orange-400" : pct >= 30 ? "bg-blue-500" : "bg-emerald-500";

  return (
    <div className={className}>
      {label && <div className="text-xs font-medium text-muted-foreground mb-1">{label}</div>}
      <div className="relative h-6 w-full bg-secondary rounded overflow-hidden border border-border">
        <div className={`absolute left-0 top-0 h-full ${color} transition-all`} style={{ width: `${pct}%` }} />
        <div className="absolute inset-0 flex items-center justify-center text-xs font-mono">
          {usedN.toLocaleString()} / {totalN.toLocaleString()} ({pct}%)
        </div>
      </div>
      <div className="flex gap-4 text-xs text-muted-foreground mt-1">
        <span>
          used: <span className="font-mono text-foreground">{usedN}</span>
        </span>
        <span>
          free: <span className="font-mono text-foreground">{freeN}</span>
        </span>
        <span>
          total: <span className="font-mono text-foreground">{totalN}</span>
        </span>
      </div>
    </div>
  );
}

// CIDRBreakdown — компактная таблица per-CIDR usage.
interface CIDRRow {
  cidr: string;
  total: number | string;
  used: number | string;
}

export function CIDRBreakdown({ cidrs }: { cidrs: CIDRRow[] }) {
  if (!cidrs || cidrs.length === 0) return null;
  return (
    <div className="border border-border rounded">
      <table className="w-full text-sm">
        <thead className="bg-muted/40">
          <tr>
            <th className="text-left px-3 py-1.5 font-medium">CIDR</th>
            <th className="text-right px-3 py-1.5 font-medium">used / total</th>
            <th className="px-3 py-1.5 w-1/2">utilization</th>
          </tr>
        </thead>
        <tbody>
          {cidrs.map((c) => {
            const totalN = Number(c.total);
            const usedN = Number(c.used);
            const pct = totalN > 0 ? Math.floor((usedN * 100) / totalN) : 0;
            const color =
              pct >= 90 ? "bg-red-500" : pct >= 70 ? "bg-orange-400" : pct >= 30 ? "bg-blue-500" : "bg-emerald-500";
            return (
              <tr key={c.cidr} className="border-t border-border">
                <td className="px-3 py-1.5 font-mono">{c.cidr}</td>
                <td className="px-3 py-1.5 text-right font-mono">
                  {usedN}/{totalN}
                </td>
                <td className="px-3 py-1.5">
                  <div className="relative h-3 w-full bg-secondary rounded overflow-hidden">
                    <div className={`absolute left-0 top-0 h-full ${color}`} style={{ width: `${pct}%` }} />
                  </div>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
