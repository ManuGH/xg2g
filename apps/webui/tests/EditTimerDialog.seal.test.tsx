import React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import EditTimerDialog from '../src/components/EditTimerDialog';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import * as client from '../src/client-ts';

// Mock the API client
vi.mock('../src/client-ts', async () => {
  const actual = await vi.importActual<any>('../src/client-ts');
  return {
    ...actual,
    addTimer: vi.fn(),
    updateTimer: vi.fn(),
    previewConflicts: vi.fn(),
  };
});

describe('EditTimerDialog Truth Sealing (UI-INV-TIMER-001)', () => {
  const mockTimer = {
    timerId: 'timer-123',
    name: 'Original Name',
    description: 'Original &amp; Description',
    begin: 1700000000,
    end: 1700003600,
    serviceRef: '1:0:1:...',
    state: 'scheduled' as const
  };

  const mockOnClose = vi.fn();
  const mockOnSave = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('does not call updateTimer if no changes are made (Seal Model B)', async () => {
    render(
      <EditTimerDialog
        timer={mockTimer}
        onClose={mockOnClose}
        onSave={mockOnSave}
      />
    );

    const saveBtn = screen.getByTestId('timer-edit-save');
    fireEvent.click(saveBtn);

    await waitFor(() => {
      expect(client.updateTimer).not.toHaveBeenCalled();
      expect(mockOnClose).toHaveBeenCalledOnce();
      expect(mockOnSave).not.toHaveBeenCalled();
    });
  });

  it('sends only dirty fields in the payload', async () => {
    render(
      <EditTimerDialog
        timer={mockTimer}
        onClose={mockOnClose}
        onSave={mockOnSave}
      />
    );

    const nameInput = screen.getByTestId('timer-edit-name');
    fireEvent.change(nameInput, { target: { value: 'New Name' } });

    const saveBtn = screen.getByTestId('timer-edit-save');
    fireEvent.click(saveBtn);

    await waitFor(() => {
      expect(client.updateTimer).toHaveBeenCalledOnce();
      expect(client.updateTimer).toHaveBeenCalledWith(expect.objectContaining({
        body: {
          name: 'New Name'
        }
      }));
    });
  });

  it('preserves raw description with HTML entities if unedited', async () => {
    render(
      <EditTimerDialog
        timer={mockTimer}
        onClose={mockOnClose}
        onSave={mockOnSave}
      />
    );

    // Edit name so we trigger a save
    const nameInput = screen.getByTestId('timer-edit-name');
    fireEvent.change(nameInput, { target: { value: 'Triggering Save' } });

    const saveBtn = screen.getByTestId('timer-edit-save');
    fireEvent.click(saveBtn);

    await waitFor(() => {
      expect(client.updateTimer).toHaveBeenCalledOnce();
      const call = (client.updateTimer as any).mock.calls[0][0];
      // Should NOT contain description because it was not edited
      expect(call.body).not.toHaveProperty('description');
    });
  });

  it('creates a timer with required fields and omits optional defaults', async () => {
    render(
      <EditTimerDialog
        availableServices={[
          {
            serviceRef: '1:0:1:service-1',
            id: '1:0:1:service-1',
            name: 'BBC One',
          } as any,
        ]}
        onClose={mockOnClose}
        onSave={mockOnSave}
      />
    );

    const nameInput = screen.getByTestId('timer-edit-name');
    fireEvent.change(nameInput, { target: { value: 'Morning News' } });

    const saveBtn = screen.getByTestId('timer-edit-save');
    fireEvent.click(saveBtn);

    await waitFor(() => {
      expect(client.addTimer).toHaveBeenCalledOnce();
      const call = (client.addTimer as any).mock.calls[0][0];
      expect(call.body.serviceRef).toBe('1:0:1:service-1');
      expect(call.body.name).toBe('Morning News');
      expect(call.body.begin).toEqual(expect.any(Number));
      expect(call.body.end).toEqual(expect.any(Number));
      expect(call.body.end).toBeGreaterThan(call.body.begin);
      expect(call.body).not.toHaveProperty('description');
      expect(call.body).not.toHaveProperty('enabled');
    });
  });
});
