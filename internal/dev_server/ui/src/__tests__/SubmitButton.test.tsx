import { ProjectEditButton } from '../SubmitButton';
import React from 'react';
import { render, screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';

describe('ProjectEditButton', () => {
  it('renders "Environment" when no environment is selected', () => {
    render(
      <ProjectEditButton isSubmitting={false} selectedEnvironment={null} />,
    );
    expect(screen.getByText('Environment')).toBeTruthy();
  });

  it('renders selected environment name and icon when an environment is selected', () => {
    render(
      <ProjectEditButton
        isSubmitting={false}
        selectedEnvironment="Production"
      />,
    );
    expect(screen.getByText('Production')).toBeTruthy();
    expect(screen.getByTestId('icon-bullseye-arrow')).toBeTruthy();
  });

  it('renders loading state when isSubmitting is true', () => {
    render(
      <ProjectEditButton isSubmitting={true} selectedEnvironment={null} />,
    );
    expect(screen.getByText('Updating...')).toBeTruthy();
    expect(screen.getByLabelText('loading')).toBeTruthy();
  });

  it('disables the button when isSubmitting is true', () => {
    render(
      <ProjectEditButton isSubmitting={true} selectedEnvironment={null} />,
    );
    expect(screen.getByRole('button')).toBeTruthy();
  });

  it('enables the button when isSubmitting is false', () => {
    render(
      <ProjectEditButton isSubmitting={false} selectedEnvironment={null} />,
    );
    expect(screen.getByRole('button')).toBeTruthy();
  });
});
