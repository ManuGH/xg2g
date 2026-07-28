import { describe, expect, it } from 'vitest';
import {
  applyUiSurfaceToDocument,
  classifyUiHeight,
  classifyUiSurface,
  resolveUiSurfaceState,
} from './uiSurface';

describe('uiSurface', () => {
  it('classifies width breakpoints into the expected surface bands', () => {
    expect(classifyUiSurface(390)).toBe('small');
    expect(classifyUiSurface(820)).toBe('medium');
    expect(classifyUiSurface(1280)).toBe('large');
    expect(classifyUiSurface(1720)).toBe('xlarge');
  });

  it('classifies height into comfortable, short, and compact tiers', () => {
    expect(classifyUiHeight(900)).toBe('comfortable');
    expect(classifyUiHeight(500)).toBe('short');
    expect(classifyUiHeight(430)).toBe('compact');
  });

  it('uses bottom navigation for small and medium surfaces automatically', () => {
    const small = resolveUiSurfaceState({
      width: 390,
      height: 844,
      coarsePointer: true,
      hoverNone: true,
      hostIsTv: false,
    });

    const medium = resolveUiSurfaceState({
      width: 844,
      height: 390,
      coarsePointer: true,
      hoverNone: true,
      hostIsTv: false,
    });

    expect(small.surface).toBe('small');
    expect(small.orientation).toBe('portrait');
    expect(small.navMode).toBe('bottom');

    expect(medium.surface).toBe('medium');
    expect(medium.orientation).toBe('landscape');
    expect(medium.heightClass).toBe('compact');
    expect(medium.navMode).toBe('bottom');
  });

  it('keeps rail navigation for large desktop-like surfaces and TVs', () => {
    const desktop = resolveUiSurfaceState({
      width: 1280,
      height: 800,
      coarsePointer: false,
      hoverNone: false,
      hostIsTv: false,
    });

    const tv = resolveUiSurfaceState({
      width: 520,
      height: 920,
      coarsePointer: true,
      hoverNone: true,
      hostIsTv: true,
    });

    expect(desktop.surface).toBe('large');
    expect(desktop.inputMode).toBe('fine');
    expect(desktop.navMode).toBe('rail');

    expect(tv.surface).toBe('small');
    expect(tv.navMode).toBe('rail');
  });

  it('applies surface state to document datasets', () => {
    const root = document.createElement('html');
    const state = resolveUiSurfaceState({
      width: 1720,
      height: 1024,
      coarsePointer: false,
      hoverNone: false,
      hostIsTv: false,
    });

    applyUiSurfaceToDocument(state, root);

    expect(root.dataset.uiSurface).toBe('xlarge');
    expect(root.dataset.uiOrientation).toBe('landscape');
    expect(root.dataset.uiInput).toBe('fine');
    expect(root.dataset.uiHeight).toBe('comfortable');
    expect(root.dataset.uiNavMode).toBe('rail');
  });
});
