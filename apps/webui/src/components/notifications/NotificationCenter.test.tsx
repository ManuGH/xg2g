import { render, screen, fireEvent, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { NotificationCenter } from './NotificationCenter';

describe('NotificationCenter Component', () => {
  beforeEach(() => {
    vi.stubGlobal('EventSource', class {
      onmessage: ((event: MessageEvent) => void) | null = null;
      onerror: ((event: Event) => void) | null = null;
      close() {}
    });

    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation((url: string) => {
        if (url === '/api/v3/notifications') {
          return Promise.resolve({
            ok: true,
            json: () =>
              Promise.resolve([
                {
                  id: 'notif_001',
                  householdId: 'default_household',
                  userId: 'usr_admin1',
                  type: 'approval_request',
                  title: 'Freigabe erforderlich',
                  body: "Max möchte 'Die Hard' (FSK 16) ansehen",
                  resourceId: 'appr_1001',
                  actionRequired: 'approve_content',
                  createdAt: new Date().toISOString(),
                },
              ]),
          });
        }
        return Promise.resolve({ ok: true, json: () => Promise.resolve({}) });
      })
    );
  });

  it('renders notification bell icon', async () => {
    render(<NotificationCenter />);
    expect(screen.getByTitle('Benachrichtigungen')).toBeInTheDocument();
    expect(await screen.findByText('1')).toBeInTheDocument();
  });

  it('opens notification popover menu on click', async () => {
    render(<NotificationCenter />);
    expect(await screen.findByText('1')).toBeInTheDocument();
    const bellBtn = screen.getByTitle('Benachrichtigungen');
    fireEvent.click(bellBtn);

    expect(screen.getByText('Facebook-style Notification Center')).toBeInTheDocument();
    expect(await screen.findByText(/Freigabe erforderlich/)).toBeInTheDocument();
    expect(screen.getByText('Erlauben')).toBeInTheDocument();
    expect(screen.getByText('Ablehnen')).toBeInTheDocument();
  });

  it('triggers inline approval action when Erlauben clicked', async () => {
    render(<NotificationCenter />);
    expect(await screen.findByText('1')).toBeInTheDocument();
    const bellBtn = screen.getByTitle('Benachrichtigungen');
    fireEvent.click(bellBtn);

    const approveBtn = await screen.findByText('Erlauben');
    await act(async () => {
      fireEvent.click(approveBtn);
    });

    expect(fetch).toHaveBeenCalledWith('/api/v3/household/approvals/appr_1001/approve', {
      method: 'POST',
    });
    expect(await screen.findByText('Freigabe erfolgreich erteilt.')).toBeInTheDocument();
  });
});
