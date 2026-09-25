// User-selected flight pose. Angles are offsets from the editor's original
// reference pose; position is in viewport percent. Keep that reference fixed.
export const DEFAULT_AXE_POSE = { pitch: -5.5, cant: -9.5, yaw: 24.5, roll: -7, x: 5.5, y: 6, scale: 1.01 };

export const AXE_POSE_CONTROLS = [
 { key: 'pitch', label: 'Tip forward / back', hint: 'Head toward you; handle away.', min: -90, max: 90, step: .5, unit: '°' },
 { key: 'cant', label: 'Slant back to the right', hint: 'Swing the far end left or right in depth.', min: -80, max: 80, step: .5, unit: '°' },
 { key: 'yaw', label: 'Turn the blade', hint: 'Reveal more or less of the blade face.', min: -180, max: 180, step: .5, unit: '°' },
 { key: 'roll', label: 'Rotate in frame', hint: 'Rotate the whole silhouette clockwise.', min: -180, max: 180, step: .5, unit: '°' },
 { key: 'x', label: 'Left / right', hint: '', min: -50, max: 50, step: .5, unit: '%' },
 { key: 'y', label: 'Up / down', hint: '', min: -50, max: 50, step: .5, unit: '%' },
 { key: 'scale', label: 'Size', hint: '', min: .2, max: 2, step: .01, unit: '×' },
];

export function isAxePose(value) {
 return value !== null && typeof value === 'object' &&
  Object.keys(value).length === AXE_POSE_CONTROLS.length &&
  AXE_POSE_CONTROLS.every(({ key, min, max }) =>
   typeof value[key] === 'number' && Number.isFinite(value[key]) && value[key] >= min && value[key] <= max);
}
