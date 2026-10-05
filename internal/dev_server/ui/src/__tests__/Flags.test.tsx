import Flags from '../Flags';
import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';

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
});
