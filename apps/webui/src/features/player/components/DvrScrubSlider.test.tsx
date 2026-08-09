import { render, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { DvrScrubSlider } from './DvrScrubSlider';

describe('DvrScrubSlider', () => {
  it('renders input range without preview when previewBaseUrl is null', () => {
    const onSeek = vi.fn();
    render(
      <DvrScrubSlider
        value={30}
        max={300}
        onSeek={onSeek}
        previewBaseUrl={null}
        windowStartUnix={null}
      />
    );

    const slider = screen.getByRole('slider');
    expect(slider).toBeInTheDocument();
    expect(screen.queryByText(/\d+:\d+/)).not.toBeInTheDocument();
  });

  it('shows thumbnail preview badge on focus and updates on value change', () => {
    const onSeek = vi.fn();
    render(
      <DvrScrubSlider
        value={150}
        max={300}
        onSeek={onSeek}
        previewBaseUrl="/hls/preview.jpg"
        windowStartUnix={null}
      />
    );

    const slider = screen.getByRole('slider');
    
    // Focus slider (e.g. D-pad or Tab focus)
    fireEvent.focus(slider);

    // Preview label (150s -> 2:30) should be rendered
    expect(screen.getByText('2:30')).toBeInTheDocument();

    // Value change via arrow key / step
    fireEvent.change(slider, { target: { value: '180' } });
    expect(onSeek).toHaveBeenCalledWith(180);
    expect(screen.getByText('3:00')).toBeInTheDocument();

    // Blur hides preview badge
    fireEvent.blur(slider);
    expect(screen.queryByText('3:00')).not.toBeInTheDocument();
  });
});
