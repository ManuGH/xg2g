import React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import EditTimerDialog from '../src/components/EditTimerDialog';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import * as client from '../src/client-ts';

// The hey-api SDK resolves with { error } instead of throwing on HTTP/network failures.
vi.mock('../src/client-ts', async () => {
  const actual = await vi.importActual<any>('../src/client-ts');
  return {
    ...actual,
    addTimer: vi.fn(),
    updateTimer: vi.fn(),
    previewConflicts: vi.fn(),
  };
});

describe('EditTimerDialog failure handling (SDK resolves { error } instead of throwing)', () => {
  const mockOnClose = vi.fn();
  const mockOnSave = vi.fn();

  const failure = {
    data: undefined,
    error: {
      type: 'about:blank',
      title: 'Conflict',
      status: 409,
      requestId: 'req-conflict',
      code: 'TIMER_CONFLICT',
      detail: 'Timer overlaps an existing recording',
    },
    response: { status: 409 },
  };

  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('does not close or refresh, and surfaces the error, when timer creation fails', async () => {
    (client.addTimer as any).mockResolvedValue(failure);

    render(
      <EditTimerDialog
        availableServices={[
          { serviceRef: '1:0:1:service-1', id: '1:0:1:service-1', name: 'BBC One' } as any,
        ]}
        onClose={mockOnClose}
        onSave={mockOnSave}
      />
    );

    fireEvent.change(screen.getByTestId('timer-edit-name'), { target: { value: 'Morning News' } });
    fireEvent.click(screen.getByTestId('timer-edit-save'));

    await waitFor(() => {
      expect(client.addTimer).toHaveBeenCalledOnce();
    });

    // A rejected create MUST NOT be treated as success.
    expect(mockOnSave).not.toHaveBeenCalled();
    expect(mockOnClose).not.toHaveBeenCalled();

    // The RFC7807 detail/title is surfaced to the user.
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/Timer overlaps an existing recording|Conflict/);
    });
  });

  it('does not close or refresh when timer update fails', async () => {
    (client.updateTimer as any).mockResolvedValue(failure);

    render(
      <EditTimerDialog
        timer={{
          timerId: 'timer-123',
          name: 'Original Name',
          begin: 1700000000,
          end: 1700003600,
          serviceRef: '1:0:1:service-1',
          state: 'scheduled' as const,
        }}
        onClose={mockOnClose}
        onSave={mockOnSave}
      />
    );

    fireEvent.change(screen.getByTestId('timer-edit-name'), { target: { value: 'Renamed' } });
    fireEvent.click(screen.getByTestId('timer-edit-save'));

    await waitFor(() => {
      expect(client.updateTimer).toHaveBeenCalledOnce();
    });

    expect(mockOnSave).not.toHaveBeenCalled();
    expect(mockOnClose).not.toHaveBeenCalled();
  });
});
