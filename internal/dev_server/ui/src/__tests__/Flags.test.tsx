import Flags from '../Flags';
import React, { useState } from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { LDFlagValue } from 'launchdarkly-js-client-sdk';

type Overrides = Record<string, { value: LDFlagValue; version: number }>;

const StatefulFlags = ({
  flags,
  initialOverrides,
}: {
  flags: Record<string, { value: LDFlagValue; version: number }>;
  initialOverrides: Overrides;
}) => {
  const [overrides, setOverrides] = useState<Overrides>(initialOverrides);
  return (
    <>
      <button
        onClick={() =>
          setOverrides((current) => ({
            ...current,
            'new-local-flag': { value: 'on', version: 1 },
          }))
        }
      >
        add local override
      </button>
      <button onClick={() => setOverrides({})}>
        clear overrides externally
      </button>
      <Flags
        availableVariations={{}}
        selectedProject="default"
        flags={flags}
        overrides={overrides}
        setOverrides={setOverrides}
      />
    </>
  );
};

const renderFlags = () =>
  render(
    <Flags
      availableVariations={{}}
      selectedProject="default"
      flags={{ 'synced-flag': { value: true, version: 1 } }}
      overrides={{
        'synced-flag': { value: false, version: 2 },
        'local-flag': { value: 'on', version: 1 },
      }}
      setOverrides={() => {}}
    />,
  );

describe('Flags', () => {
  it('lists overrides for flags not in the source environment', () => {
    renderFlags();
    expect(screen.getByText('local-flag')).toBeTruthy();
    expect(screen.getByText('synced-flag')).toBeTruthy();
  });

  it('filters to only local flags', () => {
    renderFlags();
    fireEvent.click(screen.getByLabelText('Only show local flags'));
    expect(screen.getByText('local-flag')).toBeTruthy();
    expect(screen.queryByText('synced-flag')).toBeNull();
  });

  const filterCheckbox = (label: string) => {
    const checkbox = screen.getByLabelText(label);
    const container = checkbox.closest('.only-show-overrides-label');
    expect(container).toBeTruthy();
    return { checkbox, container };
  };

  it('disables the overrides filter and hides the local filter when there are no overrides', () => {
    render(
      <Flags
        availableVariations={{}}
        selectedProject="default"
        flags={{ 'synced-flag': { value: true, version: 1 } }}
        overrides={{}}
        setOverrides={() => {}}
      />,
    );
    const overrides = filterCheckbox('Only show flags with overrides');
    expect(overrides.checkbox.hasAttribute('disabled')).toBe(true);
    expect(overrides.container?.classList.contains('disabled')).toBe(true);
    expect(screen.queryByLabelText('Only show local flags')).toBeNull();
  });

  it('enables the overrides filter and hides the local filter when every override is for a synced flag', () => {
    render(
      <Flags
        availableVariations={{}}
        selectedProject="default"
        flags={{ 'synced-flag': { value: true, version: 1 } }}
        overrides={{ 'synced-flag': { value: false, version: 2 } }}
        setOverrides={() => {}}
      />,
    );
    const overrides = filterCheckbox('Only show flags with overrides');
    expect(overrides.checkbox.hasAttribute('disabled')).toBe(false);
    expect(overrides.container?.classList.contains('disabled')).toBe(false);
    expect(screen.queryByLabelText('Only show local flags')).toBeNull();
  });

  describe('filter state after overrides go away', () => {
    beforeEach(() => {
      vi.stubGlobal(
        'fetch',
        vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: 'OK' }),
      );
    });

    afterEach(() => {
      vi.unstubAllGlobals();
    });

    const isChecked = (label: string) =>
      (screen.getByLabelText(label) as HTMLInputElement).checked;

    const syncedFlags = {
      'flag-a': { value: true, version: 1 },
      'flag-b': { value: true, version: 1 },
    };

    it('does not bring back the overrides filter after the last override is removed', async () => {
      render(
        <StatefulFlags
          flags={syncedFlags}
          initialOverrides={{ 'flag-a': { value: false, version: 2 } }}
        />,
      );
      fireEvent.click(screen.getByLabelText('Only show flags with overrides'));
      expect(screen.queryByText('flag-b')).toBeNull();

      fireEvent.click(screen.getByRole('button', { name: 'Remove override' }));
      await waitFor(() =>
        expect(
          screen.queryByRole('button', { name: 'Remove override' }),
        ).toBeNull(),
      );

      const switches = screen.getAllByRole('switch');
      expect(switches).toHaveLength(2);
      fireEvent.click(switches[1]);

      await waitFor(() =>
        expect(
          screen.getAllByRole('button', { name: 'Remove override' }),
        ).toHaveLength(1),
      );
      expect(screen.getByText('flag-a')).toBeTruthy();
      expect(screen.getByText('flag-b')).toBeTruthy();
      expect(isChecked('Only show flags with overrides')).toBe(false);
    });

    it('does not bring back the overrides filter after overrides are cleared outside removeOverride', () => {
      render(
        <StatefulFlags
          flags={syncedFlags}
          initialOverrides={{ 'flag-a': { value: false, version: 2 } }}
        />,
      );
      fireEvent.click(screen.getByLabelText('Only show flags with overrides'));
      expect(screen.queryByText('flag-b')).toBeNull();

      fireEvent.click(screen.getByText('clear overrides externally'));
      fireEvent.click(screen.getAllByRole('switch')[1]);

      expect(screen.getByText('flag-a')).toBeTruthy();
      expect(screen.getByText('flag-b')).toBeTruthy();
      expect(isChecked('Only show flags with overrides')).toBe(false);
    });

    it('does not bring back the local flags filter after the last local flag is removed', async () => {
      render(
        <StatefulFlags
          flags={{ 'synced-flag': { value: true, version: 1 } }}
          initialOverrides={{ 'local-flag': { value: 'on', version: 1 } }}
        />,
      );
      fireEvent.click(screen.getByLabelText('Only show local flags'));
      expect(screen.queryByText('synced-flag')).toBeNull();

      fireEvent.click(screen.getByRole('button', { name: 'Remove override' }));
      await waitFor(() => expect(screen.queryByText('local-flag')).toBeNull());
      expect(screen.queryByLabelText('Only show local flags')).toBeNull();

      fireEvent.click(screen.getByText('add local override'));

      expect(screen.getByText('new-local-flag')).toBeTruthy();
      expect(screen.getByText('synced-flag')).toBeTruthy();
      expect(isChecked('Only show local flags')).toBe(false);
    });

    it('does not bring back the local flags filter after overrides are cleared outside removeOverride', () => {
      render(
        <StatefulFlags
          flags={{ 'synced-flag': { value: true, version: 1 } }}
          initialOverrides={{ 'local-flag': { value: 'on', version: 1 } }}
        />,
      );
      fireEvent.click(screen.getByLabelText('Only show local flags'));
      expect(screen.queryByText('synced-flag')).toBeNull();

      fireEvent.click(screen.getByText('clear overrides externally'));
      fireEvent.click(screen.getByText('add local override'));

      expect(screen.getByText('new-local-flag')).toBeTruthy();
      expect(screen.getByText('synced-flag')).toBeTruthy();
      expect(isChecked('Only show local flags')).toBe(false);
    });
  });
});
