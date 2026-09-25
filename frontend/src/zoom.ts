// Turning a trackpad gesture into a zoom factor.
//
// Split out from the event wiring because the direction is the part that is
// easy to get backwards and impossible to check without hardware: a pinch
// cannot be synthesised, so the mapping is pinned down by tests instead.
//
// Both functions return a multiplier for the current scale: above 1 zooms in,
// below 1 zooms out, exactly 1 does nothing.

/**
 * How fast a wheel-reported pinch zooms. Larger is slower; 180 lands close to
 * the rate the native gesture gives on the same hardware.
 */
const WHEEL_DIVISOR = 180;

/**
 * A pinch reported as ctrl+wheel, which is what every engine except WebKit
 * does, and what a Windows precision touchpad sends.
 *
 * Fingers apart produce a negative deltaY — the same sign as scrolling up —
 * so the sign is flipped to make apart mean in. Exponential rather than linear
 * so the response is proportional: the same finger movement changes the scale
 * by the same ratio whether you are at 50% or 400%.
 */
export function wheelZoomFactor(deltaY: number): number {
  if (!Number.isFinite(deltaY)) return 1;
  return Math.exp(-deltaY / WHEEL_DIVISOR);
}

/**
 * A pinch reported by WebKit's gesture events, where `scale` is cumulative for
 * the whole gesture rather than a per-event delta. The factor is therefore the
 * ratio against the previous reading.
 */
export function gestureZoomFactor(scale: number, previous: number): number {
  if (!Number.isFinite(scale) || !Number.isFinite(previous)) return 1;
  if (scale <= 0 || previous <= 0) return 1;
  return scale / previous;
}
