import { render, screen, fireEvent } from '@testing-library/react';
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

  it('renders notification bell icon', () => {
    render(<NotificationCenter />);
    expect(screen.getByTitle('Benachrichtigungen')).toBeInTheDocument();
  });

  it('opens notification popover menu on click', async () => {
    render(<NotificationCenter />);
    const bellBtn = screen.getByTitle('Benachrichtigungen');
    fireEvent.click(bellBtn);

    expect(screen.getByText('Facebook-style Notification Center')).toBeInTheDocument();
    expect(await screen.findByText(/Freigabe erforderlich/)).toBeInTheDocument();
    expect(screen.getByText('Erlauben')).toBeInTheDocument();
    expect(screen.getByText('Ablehnen')).toBeInTheDocument();
  });

  it('triggers inline approval action when Erlauben clicked', async () => {
    render(<NotificationCenter />);
    const bellBtn = screen.getByTitle('Benachrichtigungen');
    fireEvent.click(bellBtn);

    const approveBtn = await screen.findByText('Erlauben');
    fireEvent.click(approveBtn);

    expect(fetch).toHaveBeenCalledWith('/api/v3/household/approvals/appr_1001/approve', {
      method: 'POST',
    });
  });
});
