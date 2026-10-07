import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { DropdownMenu } from './DropdownMenu';

const options = Array.from({ length: 8 }, (_, i) => ({ id: i, label: `Option ${i}`, description: 'detail' }));

function rect(top: number): DOMRect {
  return { top, bottom: top + 44, left: 0, right: 44, width: 44, height: 44, x: 0, y: top, toJSON: () => ({}) } as DOMRect;
}

function renderInBox(boxStyle: React.CSSProperties) {
  const { container } = render(
    <div data-testid="box" style={boxStyle}>
      <DropdownMenu icon={<span>⚙</span>} options={options} activeId={0} onSelect={() => {}} title="Qualität" />
    </div>,
  );
  const box = screen.getByTestId('box');
  const menuRoot = box.firstElementChild as HTMLElement;
  return { container, box, menuRoot };
}

describe('DropdownMenu popup height', () => {
  afterEach(() => vi.restoreAllMocks());

  it('caps the upward popup to the space below the clipping ancestor', () => {
    const { box, menuRoot } = renderInBox({ overflow: 'hidden' });
    vi.spyOn(box, 'getBoundingClientRect').mockReturnValue(rect(100));
    vi.spyOn(menuRoot, 'getBoundingClientRect').mockReturnValue(rect(300));

    fireEvent.click(screen.getByTitle('Qualität'));

    const popup = screen.getByText('Option 0').closest('button')!.parentElement!.parentElement as HTMLElement;
    // 300 (trigger top) - 100 (clip top) - 12 (gap) - 8 (margin)
    expect(popup.style.maxHeight).toBe('180px');
  });

  it('falls back to the viewport when nothing clips', () => {
    const { menuRoot } = renderInBox({});
    vi.spyOn(menuRoot, 'getBoundingClientRect').mockReturnValue(rect(300));

    fireEvent.click(screen.getByTitle('Qualität'));

    const popup = screen.getByText('Option 0').closest('button')!.parentElement!.parentElement as HTMLElement;
    expect(popup.style.maxHeight).toBe('280px');
  });
});
