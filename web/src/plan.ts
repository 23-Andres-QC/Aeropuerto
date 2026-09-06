import plan from "./plan.json";
export { plan };
export function toPlan(p: number[]): number[] {
  const east =
      (p[0]! - plan.origin[0]!) *
      111320 *
      Math.cos((plan.origin[1]! * Math.PI) / 180),
    north = (p[1]! - plan.origin[1]!) * 111320;
  return [
    east * Math.sin(plan.angle) + north * Math.cos(plan.angle) - plan.minX,
    east * Math.cos(plan.angle) - north * Math.sin(plan.angle) - plan.minY,
  ];
}
export function fromPlan(p: number[]): number[] {
  const a = p[0]! + plan.minX,
    b = p[1]! + plan.minY;
  return [
    plan.origin[0]! +
      (a * Math.sin(plan.angle) + b * Math.cos(plan.angle)) /
        (111320 * Math.cos((plan.origin[1]! * Math.PI) / 180)),
    plan.origin[1]! +
      (a * Math.cos(plan.angle) - b * Math.sin(plan.angle)) / 111320,
  ];
}
export const planAngleDeg = (plan.angle * 180) / Math.PI;
// Compass bearing (0 = north, clockwise) to a direction vector in plan units.
export function bearingVector(bearing: number, length: number): number[] {
  const a = ((bearing - planAngleDeg) * Math.PI) / 180;
  return [length * Math.cos(a), length * Math.sin(a)];
}
export function svgPath(g: GeoJSON.Geometry): string {
  const line = (ps: number[][], closed = false) =>
    ps.map((p, i) => `${i ? "L" : "M"}${toPlan(p).join(",")}`).join(" ") +
    (closed ? "Z" : "");
  if (g.type === "Polygon")
    return g.coordinates.map((r) => line(r, true)).join(" ");
  if (g.type === "LineString") return line(g.coordinates);
  return "";
}
