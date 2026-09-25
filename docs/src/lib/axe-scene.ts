/** One continuous flight shot, drawn in ink and sampled into ASCII. */
import { DEFAULT_AXE_POSE } from './axe-pose.mjs';
export type AxePoseSettings = typeof DEFAULT_AXE_POSE;
// Times are relative to the start of the throw, including the title contact.
export const AXE_FLIGHT = { impact: 1.6, spinEnd: 1.7, arrival: 2.1, orbitStart: .85, orbitEnd: 2.45 };
const SPIN_BRAKING_FRACTION = .45;

const clamp = (n: number, min = 0, max = 1) => Math.max(min, Math.min(max, n));
const ease = (n: number) => { const t = clamp(n); return t * t * (3 - 2 * t); };
const settleEase = (n: number) => { const t = clamp(n); return t * t * t * (t * (t * 6 - 15) + 10); };
const mix = (a: number, b: number, p: number) => a + (b - a) * p;
const noise = (n: number) => { const v = Math.sin(n * 127.1 + 311.7) * 43758.5453; return v - Math.floor(v); };
const gray = (n: number) => { const v = Math.round(n); return `rgb(${v} ${v} ${v})`; };
type Point = [number, number, number?];
type Vector = [number, number, number];
type Pose = { x: number; y: number; scale: number; face: number; roll: number; bend: number; pitch?: number; tumble?: number; yaw?: number; cant?: number; orbit?: number };
type Key = Pick<Pose, 'scale' | 'face' | 'roll' | 'bend'> & { at: number };

// Pitch tips the head toward the lens; roll is only the slight lean within the frame.
const FORWARD_PITCH = 1.04;
const CAMERA_DISTANCE = 520;
const HERO: Pose = { x: 0, y: 0, scale: 1, face: .46, roll: .16, bend: 0, pitch: 1.52 };
const HAFT_EYE: Point = [-3, -116];
const HAFT_GRIP: Point = [-20, 180];
const ORBIT_FOCUS: Point = [18, -48];
const ORBIT_ANGLE = 25 * Math.PI / 180;
// Depth controls the entrance along the same ray the handle follows after the spin.
const APPROACH: Key[] = [
 { at: 0, scale: .025, face: .49, roll: HERO.roll - .07, bend: 0 },
 { at: .3, scale: .04, face: .49, roll: HERO.roll - .07, bend: -1 },
 { at: .62, scale: .08, face: .485, roll: HERO.roll - .06, bend: -2 },
 { at: .94, scale: .18, face: .48, roll: HERO.roll - .04, bend: -4 },
 { at: 1.22, scale: .39, face: .475, roll: HERO.roll - .01, bend: -8 },
 { at: 1.48, scale: .74, face: .46, roll: HERO.roll + .03, bend: -12 },
 { at: AXE_FLIGHT.spinEnd, scale: 1.03, face: .45, roll: HERO.roll + .03, bend: -6 },
 { at: 1.9, scale: .994, face: .465, roll: HERO.roll - .01, bend: 3 },
 { at: AXE_FLIGHT.arrival, ...HERO },
];
function poseAt(time: number): Pose {
 const b = APPROACH.findIndex(k => k.at > time);
 if (b === 0) return { ...APPROACH[0], x: 0, y: 0 };
 if (b < 0) return HERO;
 const a = APPROACH[b - 1], next = APPROACH[b], p = (time - a.at) / (next.at - a.at);
 return { x: 0, y: 0, scale: mix(a.scale, next.scale, p), face: mix(a.face, next.face, p), roll: mix(a.roll, next.roll, p), bend: mix(a.bend, next.bend, p) };
}

function throwSpin(time: number): number {
 const p = clamp(time / AXE_FLIGHT.spinEnd);
 if (p === 1) return 0;
 // One complete turn. Brake while still approaching and finish at the depth
 // overshoot, so no rotation remains once the axe is holding near the viewer.
 const brakingDuration = 1 - SPIN_BRAKING_FRACTION;
 const braking = clamp((p - SPIN_BRAKING_FRACTION) / brakingDuration);
 // Integral of 1 - smoothstep: continuous speed and acceleration at both ends.
 const brakingTravel = braking - braking ** 3 + braking ** 4 / 2;
 const travel = p < SPIN_BRAKING_FRACTION ? p : SPIN_BRAKING_FRACTION + brakingDuration * brakingTravel;
 return Math.PI * 2 * travel / (SPIN_BRAKING_FRACTION + brakingDuration / 2);
}

const HEAD: Point[] = [
 [-30, -135], [12, -139], [44, -146], [82, -160], [117, -160],
 [128, -142], [138, -110], [141, -78], [137, -43], [124, -10],
 [107, 12], [87, 8], [78, -15], [74, -43], [58, -68], [29, -83], [-29, -86],
];
const HAFT: Point[] = [
 [-9, -144], [8, -144], [12, -103], [9, -28], [1, 57], [-12, 128],
 [-10, 172], [-14, 187], [-26, 189], [-33, 176], [-34, 153], [-30, 112],
 [-21, 43], [-14, -30], [-14, -104],
];
function cameraPoint([x, y, z = 0]: Point, pose: Pose, drag = 0): Vector {
 const cy = pose.yaw === undefined ? pose.face : Math.cos(pose.yaw);
 const sy = pose.yaw === undefined ? Math.sqrt(1 - pose.face * pose.face) : Math.sin(pose.yaw);
 const yawDepth = -x * sy + z * cy;
 const pitch = pose.pitch ?? FORWARD_PITCH;
 const cp = Math.cos(pitch), sp = Math.sin(pitch);
 const px = x * cy + z * sy + drag, py = y * cp - yawDepth * sp;
 const c = Math.cos(pose.roll), s = Math.sin(pose.roll);
 const rx = px * c - py * s, ry = px * s + py * c, depth = y * sp + yawDepth * cp;
 const cc = Math.cos(pose.cant ?? 0), cs = Math.sin(pose.cant ?? 0);
 return [rx * cc + depth * cs, ry, depth * cc - rx * cs];
}
// Moving the camera to its left rotates the view to the right about a fixed
// focus on the axe. The tracked focus stays put; the blade and haft gain parallax.
function orbitVector([x, y, z]: Vector, angle: number): Vector {
 const c = Math.cos(angle), s = Math.sin(angle);
 return [x * c - z * s, y, x * s + z * c];
}
function viewPoint(point: Point, pose: Pose, drag = 0): Vector {
 const projected = cameraPoint(point, pose, drag);
 if (!pose.orbit) return projected;
 const pivot = cameraPoint(ORBIT_FOCUS, pose);
 const rotated = orbitVector([projected[0] - pivot[0], projected[1] - pivot[1], projected[2] - pivot[2]], pose.orbit);
 return [rotated[0] + pivot[0], rotated[1] + pivot[1], rotated[2] + pivot[2]];
}
function mapPoint([x, y, z = 0]: Point, pose: Pose): Point {
 // Secondary motion lives in the drawing: the haft drags, whips, and springs back.
 const drag = pose.bend * Math.pow(clamp((y + 60) / 249), 2);
 if (pose.tumble) {
  // End-over-end around the head-heavy balance point, in the blade's plane.
  // Project afterward so the head and grip visibly trade near/far positions.
  const dx = x - 18, dy = y + 48;
  const c = Math.cos(pose.tumble), s = Math.sin(pose.tumble);
  x = 18 + dx * c - dy * s;
  y = -48 + dx * s + dy * c;
 }
 // Yaw presents the cutting edge. Pitch rotates in depth: the head comes closer
 // and the grip recedes, becoming shorter and smaller through perspective.
 const [px, py, depth] = viewPoint([x, y, z], pose, drag);
 const perspective = CAMERA_DISTANCE / (CAMERA_DISTANCE + depth);
 return [pose.x + px * perspective * pose.scale, pose.y + py * perspective * pose.scale];
}
// Center the complete silhouette, rather than its handle pivot.
const silhouette = [...HEAD, ...HAFT].map(p => mapPoint(p, HERO));
const left = Math.min(...silhouette.map(p => p[0])), right = Math.max(...silhouette.map(p => p[0]));
const top = Math.min(...silhouette.map(p => p[1])), bottom = Math.max(...silhouette.map(p => p[1]));
const ART_CENTER: Point = [(left + right) / 2, (top + bottom) / 2];
function framePoint(point: Point, pose: Pose): Point {
 const [x, y] = mapPoint(point, pose);
 return [x - ART_CENTER[0] * pose.scale, y - ART_CENTER[1] * pose.scale];
}
function selectedPose(settings: AxePoseSettings, width: number, height: number, artScale: number): Pose {
 const radians = Math.PI / 180;
 return {
  ...HERO,
  pitch: HERO.pitch! + settings.pitch * radians,
  yaw: Math.acos(HERO.face) + settings.yaw * radians,
  cant: settings.cant * radians,
  roll: HERO.roll + settings.roll * radians,
  x: settings.x / 100 * width / artScale,
  y: settings.y / 100 * height / artScale,
  scale: settings.scale,
 };
}
// The entrance and wind share the haft's vanishing point. Carry that direction
// through the camera move too, so the air never slips away from the flight axis.
function throwOrigin(pose: Pose): Point {
 const axis = orbitVector(cameraPoint([HAFT_GRIP[0] - HAFT_EYE[0], HAFT_GRIP[1] - HAFT_EYE[1]], pose), pose.orbit ?? 0);
 // Only the editor can turn the haft exactly side-on to the camera.
 const depth = Math.abs(axis[2]) < .01 ? (axis[2] < 0 ? -.01 : .01) : axis[2];
 return [
  pose.x + (CAMERA_DISTANCE * axis[0] / depth - ART_CENTER[0]) * pose.scale,
  pose.y + (CAMERA_DISTANCE * axis[1] / depth - ART_CENTER[1]) * pose.scale,
 ];
}

export function createAxeScene(ink: CanvasRenderingContext2D) {
 const polygon = (points: Point[], tone: number, stroke = 0, width = 0) => {
  ink.beginPath(); points.forEach(([x, y], i) => i ? ink.lineTo(x, y) : ink.moveTo(x, y));
  ink.closePath(); ink.fillStyle = gray(tone); ink.fill();
  if (width) { ink.strokeStyle = gray(stroke); ink.lineWidth = width; ink.lineJoin = 'round'; ink.stroke(); }
 };
 const line = (points: Point[], tone: number, width = 2, directional = false) => {
  ink.beginPath(); points.forEach(([x, y], i) => i ? ink.lineTo(x, y) : ink.moveTo(x, y));
  // Color encodes a glyph direction only. The player always displays neutral gray.
  const a = points[0], b = points[points.length - 1], dx = b[0] - a[0], dy = b[1] - a[1];
  const direction = Math.abs(dy) < Math.abs(dx) * .35 ? 0 : Math.abs(dx) < Math.abs(dy) * .35 ? .9 : dx * dy < 0 ? .3 : .6;
  ink.strokeStyle = directional ? `rgb(${tone} 0 ${Math.round(tone * direction)})` : gray(tone);
  ink.lineWidth = width; ink.lineCap = 'round'; ink.stroke();
 };
 const axe = (pose: Pose, silhouette = false) => {
  const map = (p: Point) => framePoint(p, pose);
  // A shallow forged body, thickest at the socket and tapered at the cutting
  // edge, gives the head weight even when the camera sees it almost edge-on.
  const thickness = ([x]: Point) => mix(6.5, 1, ease((x - 15) / 120));
  const normal = orbitVector(cameraPoint([0, 0, 1], pose), pose.orbit ?? 0);
  const face = normal[2] > 0 ? -1 : 1;
  const surface = (p: Point, side: number): Point => [p[0], p[1], thickness(p) * side];
  const head = (p: Point[]) => p.map(p => map(surface(p, face)));
  const weight = Math.max(2, pose.scale * 3);
  polygon(HAFT.map(map), silhouette ? 44 : 141, 18, weight);
  if (!silhouette) {
   line([[-3, -120], [-3, -27], [-12, 55], [-22, 135], [-20, 180]].map(p => map(p as Point)), 179, weight * 1.7);
   for (let y = 76; y < 174; y += 15) {
    const x = -9 - (y - 76) * .15;
    line([[x - 13, y], [x + 5, y - 5]].map(p => map(p as Point)), 36, weight * 1.9);
   }
  }
  polygon(HEAD.map(p => map(surface(p, -face))), silhouette ? 44 : 78, 16, weight);
  const rim = HEAD.map((a, index) => {
   const b = HEAD[(index + 1) % HEAD.length];
   const points = [surface(a, -1), surface(b, -1), surface(b, 1), surface(a, 1)];
   return { points, depth: points.reduce((sum, p) => sum + viewPoint(p, pose)[2], 0) / 4, index };
  }).sort((a, b) => b.depth - a.depth);
  for (const { points, index } of rim) polygon(points.map(map), silhouette ? 50 : index < 5 ? 158 : 92, 24, weight * .4);
  polygon(head(HEAD), silhouette ? 55 : 126, 16, weight * 2);
  if (silhouette) return;
  // Flat ink shadows and a broad curved bevel replace smoothly shaded mesh facets.
  polygon(head([[-29, -128], [22, -127], [66, -135], [83, -114], [63, -84], [29, -88], [-29, -91]]), 69);
  const bevel: Point[] = [...HEAD.slice(3, 12), [94, -7], [109, -32], [121, -75], [117, -118], [101, -145], [82, -151]];
  polygon(head(bevel), 203);
  line(head(HEAD.slice(4, 11)), 237, weight * 1.7);
  line(head([[22, -114], [43, -93], [63, -118], [84, -91]]), 51, weight);
  line(head([[24, -100], [44, -126], [64, -99], [84, -122]]), 51, weight);
  line(head([[-27, -136], [12, -138], [45, -145], [82, -160], [112, -159]]), 179, weight);
 };

 const wind = (time: number, strength: number, width: number, height: number, artScale: number, origin: Point) => {
  const center: Point = [width / 2 + origin[0] * artScale, height / 2 + origin[1] * artScale];
  const reach = Math.hypot(width, height) * .72;
  for (let i = 0; i < 52; i++) {
   const angle = noise(i + 7) * Math.PI * 2;
   // Looking back at the approaching axe, air recedes toward the vanishing point.
   const phase = 1 - (noise(i + 31) + time * (.32 + noise(i + 41) * .22)) % 1;
   const radius = artScale * 105 + phase * reach;
   const tail = radius + artScale * (70 + noise(i + 11) * 150);
   const dx = Math.cos(angle), dy = Math.sin(angle);
   ink.save(); ink.globalAlpha = strength * Math.sin(phase * Math.PI);
   line([[center[0] + dx * radius, center[1] + dy * radius], [center[0] + dx * tail, center[1] + dy * tail]], 100 + Math.floor(noise(i) * 65), Math.max(2.5, artScale * (1.5 + noise(i + 2) * 1.7)), true);
   ink.restore();
  }
 };

 return (time: number, still = false, windTime = time, editorPose?: AxePoseSettings) => {
  const { width, height } = ink.canvas;
  // Fit the axe separately from the full-screen wind, preserving its proportions.
  const artScale = Math.min(width * .62 / (right - left), height * .57 / (bottom - top));
  const held = selectedPose(editorPose ?? DEFAULT_AXE_POSE, width, height, artScale);
  const origin = throwOrigin(held);
  const orbit = editorPose ? 0 : ORBIT_ANGLE * (still ? 1 : ease((time - AXE_FLIGHT.orbitStart) / (AXE_FLIGHT.orbitEnd - AXE_FLIGHT.orbitStart)));
  const viewedOrigin = throwOrigin({ ...held, orbit });
  const vanishingPoint = { x: .5 + viewedOrigin[0] * artScale / width, y: .5 + viewedOrigin[1] * artScale / height };
  ink.clearRect(0, 0, width, height);
  const progress = clamp(time / AXE_FLIGHT.arrival);
  const rush = ease((progress - .4) / .25) * (1 - ease((progress - .8) / .2));
  if (!still || editorPose) wind(windTime, .34 + ease(progress) * .12 + rush * .26, width, height, artScale, viewedOrigin);
  // Negative flight time is the opening typography, with wind but no axe yet.
  if (time < 0) return vanishingPoint;
  const entry = (t: number): Pose => {
   const p = poseAt(t);
   // Steer during the last spinning arc, reaching the selected angle exactly
   // when the tumble ends. There is no stopped pose followed by a second turn.
   const settleStart = AXE_FLIGHT.spinEnd * SPIN_BRAKING_FRACTION;
   const settle = settleEase((t - settleStart) / (AXE_FLIGHT.spinEnd - settleStart));
   return {
    ...p,
    x: mix(origin[0], held.x, p.scale),
    y: mix(origin[1], held.y, p.scale),
    scale: p.scale * held.scale,
    pitch: mix(FORWARD_PITCH, held.pitch!, settle),
    yaw: mix(Math.acos(p.face), held.yaw!, settle),
    cant: held.cant! * settle,
    roll: mix(p.roll, held.roll, settle),
    tumble: throwSpin(t),
    // Carry the same camera through the approach and held pose without a jump.
    orbit,
   };
  };
  const age = Math.max(0, time - AXE_FLIGHT.arrival);
  const arrived = time >= AXE_FLIGHT.arrival;
  let pose = entry(time);
  if (arrived || still) {
   const amount = still ? 0 : ease(age / .45);
   pose = {
    ...held,
    x: held.x + amount * (Math.sin(age * 7.3) * 1.3 + Math.sin(age * 13.1) * .45),
    y: held.y + amount * (Math.sin(age * 5.7) * 1.6 + Math.sin(age * 11.3) * .5),
    roll: held.roll + amount * (Math.sin(age * 3.1) * .012 + Math.sin(age * 9.7) * .004),
    yaw: held.yaw! + amount * Math.sin(age * 2.3) * .008,
    bend: amount * Math.sin(age * 7.3) * 2.7,
    scale: held.scale * (1 + amount * Math.sin(age * 2.1) * .004),
    orbit,
   };
  }
  if (editorPose) {
   pose = held;
  }
  ink.save(); ink.translate(width / 2, height / 2); ink.scale(artScale, artScale);
  if (!still && progress > .4 && progress < .9) {
   // A brief drawn trail gives the entrance momentum; it clears for the held pose.
   const trail = [.11, .06, 0].map(lag => entry(Math.max(0, time - lag)));
   ink.save(); ink.globalAlpha = .5 * (1 - ease((progress - .7) / .2));
   const upper = trail.map(p => framePoint([75, -148], p));
   const lower = [...trail].reverse().map(p => framePoint([94, -12], p));
   polygon([...upper, ...lower], 43);
   line(upper, 90, 3, true);
   axe(trail[0], true);
   ink.restore();
  }
  axe(pose);
  ink.restore();
  return vanishingPoint;
 };
}
