import { describe, expect, it } from 'vitest';
import { gestureZoomFactor, wheelZoomFactor } from './zoom';

// The whole point of these tests: a pinch cannot be synthesised in a test or by
// driving the app, so the direction is asserted here rather than discovered by
// a reader whose document shrank when they tried to enlarge it.

describe('pinch reported as ctrl+wheel', () => {
  it('zooms in when the fingers move apart', () => {
    // Apart is reported as a negative deltaY, the same sign as scrolling up.
    expect(wheelZoomFactor(-40)).toBeGreaterThan(1);
  });

  it('zooms out when the fingers move together', () => {
    expect(wheelZoomFactor(40)).toBeLessThan(1);
  });

  it('does nothing when there is no movement', () => {
    expect(wheelZoomFactor(0)).toBe(1);
  });

  // Proportional, not additive: pinching the same distance has to feel the same
  // whether the page is at 50% or 400%.
  it('is symmetric, so a pinch out then back in returns to the start', () => {
    expect(wheelZoomFactor(-40) * wheelZoomFactor(40)).toBeCloseTo(1, 10);
  });

  it('scales with the distance moved', () => {
    expect(wheelZoomFactor(-80)).toBeGreaterThan(wheelZoomFactor(-40));
  });

  it('survives a garbage delta rather than producing NaN', () => {
    expect(wheelZoomFactor(Number.NaN)).toBe(1);
    expect(wheelZoomFactor(Number.POSITIVE_INFINITY)).toBe(1);
  });
});

describe("pinch reported by WebKit's gesture events", () => {
  it('zooms in when the cumulative scale grows', () => {
    expect(gestureZoomFactor(1.2, 1)).toBeGreaterThan(1);
  });

  it('zooms out when the cumulative scale shrinks', () => {
    expect(gestureZoomFactor(0.8, 1)).toBeLessThan(1);
  });

  // `scale` is cumulative for the gesture, so treating it as a per-event delta
  // would compound it and send the zoom to a limit within a few frames.
  it('reports the step against the previous reading, not against 1', () => {
    expect(gestureZoomFactor(1.5, 1.5)).toBe(1);
    expect(gestureZoomFactor(2, 1.5)).toBeCloseTo(4 / 3, 10);
  });

  it('composes across a gesture to the total scale', () => {
    const readings = [1.1, 1.3, 1.6, 2];
    let previous = 1;
    let total = 1;
    for (const reading of readings) {
      total *= gestureZoomFactor(reading, previous);
      previous = reading;
    }
    expect(total).toBeCloseTo(2, 10);
  });

  it('ignores a nonsensical reading rather than collapsing the scale', () => {
    expect(gestureZoomFactor(0, 1)).toBe(1);
    expect(gestureZoomFactor(1.2, 0)).toBe(1);
    expect(gestureZoomFactor(Number.NaN, 1)).toBe(1);
  });
});
