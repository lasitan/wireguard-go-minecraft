import type { Mesh, PlacedNode } from "../../core/models";
import { subnetZones } from "../../topology/SubnetGroups";

export function SubnetZones({ mesh, placed }: { mesh: Mesh; placed: PlacedNode[] }) {
  const zones = subnetZones(mesh, placed);
  if (!zones) return null;
  return (
    <g className="subnet-zones" aria-hidden>
      {zones.map((z) => (
        <g key={z.prefix} className="subnet-zone" data-prefix={z.prefix}>
          <rect x={z.minX} y={z.minY} width={z.maxX - z.minX} height={z.maxY - z.minY} rx={16} />
          <text x={z.minX + 12} y={z.minY + 20} className="subnet-zone-label">
            {z.prefix}
          </text>
        </g>
      ))}
    </g>
  );
}
