import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';

const { getSystemHealth, postSystemRefresh } = vi.hoisted(() => ({
  getSystemHealth: vi.fn(),
  postSystemRefresh: vi.fn(),
}));

vi.mock('../client-ts', () => ({
  getSystemHealth,
  postSystemRefresh,
}));

import Files from './Files';

// The M3U, XMLTV and HDHomeRun endpoints left with the legacy HTTP surface and
// answer 404. The page must not hand out links or addresses that lead there.
const REMOVED_PATHS = ['/files/playlist.m3u', '/xmltv.xml', '/device.xml'];

describe('Files', () => {
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('keeps regeneration but advertises none of the removed feed endpoints', async () => {
    getSystemHealth.mockResolvedValue({
      data: { status: 'ok', epg: { status: 'ok', missingChannels: 0 } },
    });

    const { container } = render(
      <MemoryRouter>
        <Files />
      </MemoryRouter>
    );

    expect(await screen.findByRole('button', { name: /regenerate|neu erzeugen/i })).toBeInTheDocument();

    const text = container.textContent ?? '';
    const hrefs = Array.from(container.querySelectorAll('a')).map((a) => a.getAttribute('href') ?? '');
    for (const path of REMOVED_PATHS) {
      expect(hrefs.some((href) => href.includes(path)), `link to ${path}`).toBe(false);
      expect(text.includes(path), `address ${path} in the page text`).toBe(false);
    }
    expect(text).not.toMatch(/HDHomeRun/i);
  });
});
