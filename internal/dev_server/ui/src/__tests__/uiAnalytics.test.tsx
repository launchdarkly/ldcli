import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, useNavigate } from 'react-router';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import Flags from '../Flags';
import { PageAnalytics } from '../App';

type FetchCall = { url: string; body?: string };

const calls: FetchCall[] = [];
let putStatus = 200;
let analyticsFails = false;

function installFetch() {
  calls.length = 0;
  putStatus = 200;
  analyticsFails = false;
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const body = typeof init?.body === 'string' ? init.body : undefined;
      calls.push({ url, body });
      if (url.includes('/ui-analytics')) {
        if (analyticsFails) {
          throw new Error('analytics down');
        }
        return new Response(null, { status: 204 });
      }
      return new Response('{}', { status: putStatus });
    }),
  );
}

function GoToEvents() {
  const navigate = useNavigate();
  return <button onClick={() => navigate('/ui/events')}>Go</button>;
}

describe('dev server UI analytics', () => {
  beforeEach(() => {
    installFetch();
  });

  it('posts a page view when the page changes', async () => {
    render(
      <MemoryRouter initialEntries={['/ui/flags']}>
        <PageAnalytics />
        <GoToEvents />
      </MemoryRouter>,
    );

    await waitFor(() => {
      expect(calls.some((call) => call.body?.includes('"page":"flags"'))).toBe(
        true,
      );
    });

    fireEvent.click(screen.getByRole('button', { name: 'Go' }));

    await waitFor(() => {
      expect(
        calls.some((call) => call.body?.includes('"page":"events"')),
      ).toBe(true);
    });

    const pageView = calls.find((call) => call.body?.includes('"page":"flags"'));
    expect(pageView?.url).toContain('/ui-analytics');
    expect(pageView?.body).toContain('Dev Server UI Page Viewed');
    expect(pageView?.body).not.toContain('proj');
  });

  it('sends the override type after the PUT and not the flag value', async () => {
    let overrides: Record<string, { value: unknown; version: number }> = {};
    render(
      <Flags
        availableVariations={{ 'my-flag': [] }}
        selectedProject="proj"
        flags={{ 'my-flag': { value: false } }}
        overrides={{}}
        setOverrides={(next) => {
          overrides = next;
        }}
      />,
    );

    fireEvent.click(screen.getByRole('switch'));

    await waitFor(() => {
      expect(calls.some((call) => call.url.includes('/ui-analytics'))).toBe(
        true,
      );
    });

    const putIndex = calls.findIndex((call) => call.url.includes('/overrides/'));
    const analyticsIndex = calls.findIndex((call) =>
      call.url.includes('/ui-analytics'),
    );
    expect(putIndex).toBeGreaterThanOrEqual(0);
    expect(analyticsIndex).toBeGreaterThan(putIndex);

    const analytics = JSON.parse(calls[analyticsIndex].body || '{}');
    expect(analytics.event).toBe('Dev Server UI Flag Override Set');
    expect(analytics.properties.value_kind).toBe('boolean');
    expect(analytics.properties.control).toBe('switch');
    expect(analytics.properties.outcome).toBe('success');
    expect(analytics.properties.value).toBeUndefined();
    expect(JSON.stringify(analytics)).not.toContain('my-flag');
    expect(overrides['my-flag'].value).toBe(true);
  });

  it('keeps the override when the analytics post fails', async () => {
    analyticsFails = true;
    let overrides: Record<string, { value: unknown; version: number }> = {};
    render(
      <Flags
        availableVariations={{ 'my-flag': [] }}
        selectedProject="proj"
        flags={{ 'my-flag': { value: false } }}
        overrides={{}}
        setOverrides={(next) => {
          overrides = next;
        }}
      />,
    );

    fireEvent.click(screen.getByRole('switch'));

    await waitFor(() => {
      expect(calls.some((call) => call.url.includes('/overrides/'))).toBe(true);
    });
    await waitFor(() => {
      expect(overrides['my-flag']?.value).toBe(true);
    });
  });

  it('sends an error outcome and restores the override when the PUT fails', async () => {
    putStatus = 500;
    let overrides: Record<string, { value: unknown; version: number }> = {
      keep: { value: false, version: 0 },
    };
    render(
      <Flags
        availableVariations={{ 'my-flag': [] }}
        selectedProject="proj"
        flags={{ 'my-flag': { value: false } }}
        overrides={{}}
        setOverrides={(next) => {
          overrides = next;
        }}
      />,
    );

    fireEvent.click(screen.getByRole('switch'));

    await waitFor(() => {
      expect(calls.some((call) => call.body?.includes('"outcome":"error"'))).toBe(
        true,
      );
    });
    expect(overrides['my-flag']).toBeUndefined();
  });
});
